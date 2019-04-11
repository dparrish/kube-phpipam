![Build status](https://travis-ci.com/dparrish/kube-phpipam.svg?branch=master)

# Kube-phpIPAM

This controller listens to the Kubernetes API for changes in Services. Services
with IP addresses matching a configurable set will have their IP addresses
updated in an external [phpIPAM](https://phpipam.net) instance.

## Create configuration

There is a sample config.yaml file included in this repository. You will need
to change the details to point to your phpIPAM instance and provide a real
username and password that must be created as a user in phpIPAM.

Once you've created the config file, add it as a configmap in K8s:

```shell
$ kubectl create configmap kube-phpipam-config --from-file=config.yaml
```

## Run kube-phpipam

A Kubernetes yaml file is included that will create a service account with the
appropriate permissions, and start kube-phpipam as a single-pod deployment.

```shell
$ kubectl apply -f kube-phpipam.yaml
```

## Build a container

If you want to use HTTPS to talk to phpIPAM you may need to build a custom
image containing the SSL certificates. Support for LetsEncrypt X3 certificates
are already built in. You can do that with a simple Dockerfile
such as this:

```docker
FROM dparrish/kube-phpipam:1.1.0
COPY *.crt /usr/local/share/ca-certificates/
RUN update-ca-certificates
```

## Configurable templates

The hostname and description generated in phpIPAM can be configured using Go
[text/template](https://golang.org/pkg/text/template/) style templates.

The default templates only set the service name as the hostname.

In the config file, add either a `nameTemplate` or `descriptionTemplate` to a
subnet. The object passed to the template expansion is a [k8s
corev1.Service](https://godoc.org/k8s.io/api/core/v1#Service), so any fields
there are available for use.

For example, the author uses MetalLB as the baremetal load balancer
implementation, and the `metallb.universe.tf/allow-shared-ip` annotation on
services to set a specific hostname. The following template makes use of that.

```
subnets:
  - cidr: 10.4.0.0/24
    type: LoadBalancer
    nameTemplate: |-
      {{ if index .Annotations "metallb.universe.tf/allow-shared-ip" }}
        {{ index .Annotations "metallb.universe.tf/allow-shared-ip" -}}
      {{ else }}
        {{.Name -}}
      {{ end -}}
      . {{- .Namespace}}
```
