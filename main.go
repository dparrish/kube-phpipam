package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	_ "k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	_ "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	_ "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/dparrish/kube-phpipam/phpipam"
	"github.com/spotahome/kooper/log"
	"github.com/spotahome/kooper/operator/controller"
	"github.com/spotahome/kooper/operator/handler"
	"github.com/spotahome/kooper/operator/retrieve"
	"gopkg.in/yaml.v2"
)

var (
	configFile = flag.String("config", "config.yaml", "Path to configuration YAML file")

	subnetCache = make(map[string]*phpipam.Subnet)
	ipCache     = make(map[string]string)
)

func getSubnet(client *phpipam.Client, cidr string) (*phpipam.Subnet, error) {
	if x, ok := subnetCache[cidr]; ok {
		return x, nil
	}
	var subnets phpipam.SubnetsResponse
	if err := client.GET("subnets/cidr/"+cidr, &subnets); err != nil {
		return nil, err
	}
	if len(subnets.Data) == 0 {
		return nil, fmt.Errorf("no subnet found matching CIDR %s", cidr)
	}
	if len(subnets.Data) > 1 {
		return nil, fmt.Errorf("multiple subnet found matching CIDR %s", cidr)
	}
	subnetCache[cidr] = &subnets.Data[0]
	return &subnets.Data[0], nil
}

func createIP(client *phpipam.Client, subnet, ip, hostname string) error {
	var out phpipam.IPAddressPatchResponse
	url := fmt.Sprintf("addresses/")
	if err := client.POST(url, map[string]interface{}{
		"ip":       ip,
		"hostname": hostname,
		"subnetId": subnet,
		"note":     "Added by kube-phpipam",
	}, &out); err != nil {
		return fmt.Errorf("error adding IP address %q: %v", ip, err)
	}
	if out.Code < 200 || out.Code > 299 {
		return fmt.Errorf("Error from phpIPAM adding IP address %q: %v", ip, out.Message)
	}
	ipCache[ip] = hostname
	return nil
}

func getIP(client *phpipam.Client, ip string) (*phpipam.IPAddress, error) {
	var res phpipam.IPAddressResponse
	if err := client.GET(fmt.Sprintf("addresses/search/%s/", ip), &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("%s", res.Message)
	}
	if len(res.Data) == 0 {
		return nil, nil
	}
	if len(res.Data) > 1 {
		return nil, fmt.Errorf("multiple IPs found matching %s", ip)
	}
	return &res.Data[0], nil
}

func setHostname(client *phpipam.Client, id, ip, hostname string) error {
	var out phpipam.IPAddressPatchResponse
	url := fmt.Sprintf("addresses/%s/", id)
	if err := client.PATCH(url, map[string]string{
		"hostname": hostname,
		"note":     "Added by kube-phpipam",
	}, &out); err != nil {
		return err
	}
	if out.Code < 200 || out.Code > 299 {
		return fmt.Errorf("%s", out.Message)
	}
	ipCache[ip] = hostname
	return nil
}

func main() {
	flag.Parse()
	log := &log.Std{}
	cfg, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Errorf("Unable to read configuration file %s: %v", *configFile, err)
		os.Exit(1)
	}
	var config Config
	if err := yaml.Unmarshal(cfg, &config); err != nil {
		log.Errorf("Unable to read configuration file %s: %v", *configFile, err)
		os.Exit(1)
	}

	client, err := phpipam.NewClient(context.Background(), config.PHPIpam.Host, config.PHPIpam.AppID, config.PHPIpam.Username, config.PHPIpam.Password)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	defer client.Close()

	k8scfg, err := rest.InClusterConfig()
	if err != nil {
		// No in cluster? letr's try locally
		kubehome := filepath.Join(homedir.HomeDir(), ".kube", "config")
		k8scfg, err = clientcmd.BuildConfigFromFlags("", kubehome)
		if err != nil {
			log.Errorf("error loading kubernetes configuration: %s", err)
			os.Exit(1)
		}
	}
	k8scli, err := kubernetes.NewForConfig(k8scfg)
	if err != nil {
		log.Errorf("error creating kubernetes client: %s", err)
		os.Exit(1)
	}

	retriever := &retrieve.Resource{
		Object: &corev1.Service{},
		ListerWatcher: &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return k8scli.CoreV1().Services("").List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8scli.CoreV1().Services("").Watch(options)
			},
		},
	}

	// Our domain logic that will print every add/sync/update and delete event we .
	hand := &handler.HandlerFunc{
		AddFunc: func(_ context.Context, obj runtime.Object) error {
			svc := obj.(*corev1.Service)
			log.Infof("Service added: %s/%s", svc.Namespace, svc.Name)
			hostname := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)

			if l, ok := svc.Annotations["metallb.universe.tf/allow-shared-ip"]; ok {
				hostname = fmt.Sprintf("%s/%s", svc.Namespace, l)
			}

			for _, subnet := range config.Subnets {
				if subnet.Namespace != nil && svc.Namespace != *subnet.Namespace {
					//log.Infof("skipping because of wrong namespace")
					continue
				}
				if subnet.Type != nil && svc.Spec.Type != *subnet.Type {
					//log.Infof("skipping because of wrong type")
					continue
				}
				if subnet.Regex != nil {
					re, err := regexp.Compile(*subnet.Regex)
					if err != nil {
						log.Errorf("Error compiling regex %q for subnet: %v", *subnet.Regex, err)
						continue
					}
					if !re.MatchString(svc.Name) {
						//log.Infof("skipping because of regex")
						continue
					}
				}

				_, ipnet, err := net.ParseCIDR(subnet.CIDR)
				if err != nil {
					log.Errorf("Config subnet contains invalid CIDR address %q: %v", subnet.CIDR, err)
					continue
				}

				ipamSubnet, err := getSubnet(client, subnet.CIDR)
				if err != nil {
					log.Errorf("Subnet matching %s not found in phpIPAM: %v", subnet.CIDR, err)
					continue
				}
				//log.Infof("phpIPAM subnet: %+v", ipamSubnet)

				var ips []string
				appendip := func(ip string) {
					if ip != "" && ipnet.Contains(net.ParseIP(ip)) {
						ips = append(ips, ip)
					}
				}
				appendip(svc.Spec.LoadBalancerIP)
				appendip(svc.Spec.ClusterIP)
				for _, ip := range svc.Spec.ExternalIPs {
					appendip(ip)
				}

				/*
					b, _ := json.MarshalIndent(svc.Spec, "", "  ")
					log.Infof("Spec: %s", string(b))
				*/

				for _, ip := range ips {
					var res phpipam.IPAddressResponse
					url := fmt.Sprintf("addresses/search/%s/", ip)
					//log.Infof("Looking for phpipam matching address: %s", url)
					if err := client.GET(url, &res); err != nil {
						log.Errorf("%v", err)
						continue
					}
					if res.Code != 200 {
						log.Infof("IP address %s not found in phpIPAM, adding to subnet %s with hostname %s", ip, ipamSubnet.ID, hostname)
						if err := createIP(client, ipamSubnet.ID, ip, hostname); err != nil {
							log.Errorf("Error creating IP in phpIPAM: %v", err)
						}
						continue
					}
					/*
						b, _ := json.MarshalIndent(res, "", "  ")
						log.Infof("%s", string(b))
					*/

					var found bool
					for _, foundIP := range res.Data {
						if foundIP.Hostname == nil {
							continue
						}
						if foundIP.IP == ip && *foundIP.Hostname == hostname {
							found = true
							break
						}
					}
					if found {
						//log.Infof("IP %s already has the right allocation in phpipam", ip)
						ipCache[ip] = hostname
						continue
					}

					if err := setHostname(client, res.Data[0].ID, ip, hostname); err != nil {
						log.Errorf("Error from phpIPAM patching IP address %s: %v", ip, err)
						continue
					}
				}
			}
			return nil
		},
		DeleteFunc: func(_ context.Context, s string) error {
			log.Infof("Service deleted: %s", s)
			for ip, name := range ipCache {
				if name != s {
					continue
				}
				log.Infof("Found existing IP cache for %s: %s", s, ip)

				ipamip, err := getIP(client, ip)
				if err != nil {
					log.Errorf("%v", err)
					continue
				}
				if ipamip == nil {
					// Does not exist, that's fine.
					continue
				}

				log.Infof("phpIPAM contains mapping for %s: %s", ip, ipamip.ID)
				var res phpipam.IPAddressDeleteResponse
				if err := client.DELETE(fmt.Sprintf("addresses/%s/", ipamip.ID), &res); err != nil {
					log.Errorf("Error deleting IP from phpIPAM: %v", err)
				}
				if res.Code < 200 || res.Code > 299 {
					log.Errorf("Error deleting IP from phpIPAM: %v", res.Message)
				}
				delete(ipCache, ip)
				break
			}
			return nil
		},
	}

	// Create the controller that will refresh every 30 seconds.
	ctrl := controller.New(&controller.Config{
		Name:              "kube-phpipam",
		ConcurrentWorkers: 1,
		ResyncInterval:    1 * time.Minute,
	}, hand, retriever, nil, nil, nil, log)

	// Start our controller.
	stopC := make(chan struct{})
	if err := ctrl.Run(stopC); err != nil {
		log.Errorf("error running controller: %s", err)
		os.Exit(1)
	}
	client.Close()
	<-client.Done
}
