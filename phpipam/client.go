package phpipam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

const TimeFormat = "2006-01-02 15:04:05"

type Client struct {
	Token      string
	Expires    time.Time
	username   string
	password   string
	url        string
	appid      string
	HTTPClient *http.Client
	Close      func()
	Done       <-chan struct{}
}

func NewClient(ctx context.Context, url, appid, username, password string) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)

	a := &Client{
		username:   username,
		password:   password,
		appid:      appid,
		url:        url,
		HTTPClient: &http.Client{},
		Close:      cancel,
		Done:       ctx.Done(),
	}
	if a.url[len(a.url)-1] != '/' {
		a.url += "/"
	}

	if err := a.authenticate(); err != nil {
		return nil, fmt.Errorf("initial authentication failed: %v", err)
	}

	go func() {
		for {
			// Renew the token half way before it expires.
			expires := time.Until(a.Expires) / 2
			select {
			case <-ctx.Done():
				log.Printf("Authentication goroutine has been cancelled")
				return
			case <-time.After(expires):
				log.Printf("Authentication timeout expired, retrying authentication")
				if err := a.authenticate(); err != nil {
					log.Fatalf("Unable to extend token: %v", err)
				}
			}
		}
	}()

	return a, nil
}

type AuthResponse struct {
	Code    int
	Success interface{}
	Time    float32
	Data    struct {
		Token   string
		Expires string
	}
}

type Section struct {
	ID               string   `json:"id"`
	EditDate         string   `json:"editDate"`
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	MasterSection    string   `json:"masterSection"`
	Permissions      string   `json:"permissions"`
	StrictMode       string   `json:"strictMode"`
	SubnetOrdering   string   `json:"strictMode"`
	Order            []string `json:"order"`
	ShowVLAN         string   `json:"showVLAN"`
	ShowVRF          string   `json:"showVRF"`
	DNS              []string `json:"dns"`
	ShowSupernetOnly string   `json:"showSupernetOnly"`
	Links            []struct {
		Rel     string   `json:"rel"`
		Href    string   `json:"href"`
		Methods []string `json:"methods"`
	} `json:"links"`
}

type Subnet struct {
	ID          string  `json:"id"`
	EditDate    *string `json:"editDate"`
	Description *string `json:"description"`
	Permissions *string `json:"permissions"`

	DNSrecords            string   `json:"DNSrecords"`
	DNSrecursive          string   `json:"DNSrecursive"`
	AllowRequests         string   `json:"allowRequests"`
	Device                string   `json:"device"`
	DiscoverSubnet        string   `json:"discoverSubnet"`
	FirewallAddressObject []string `json:"firewallAddressObject"`
	IP                    []string `json:"ip"`
	IsFolder              string   `json:"isFolder"`
	IsFull                string   `json:"isFull"`
	LastDiscovery         *string  `json:"lastDiscovery"`
	LastScan              *string  `json:"lastScan"`
	LinkedSubnet          []Subnet `json:"linked_subnet"`
	Links                 []struct {
		Rel     string   `json:"rel"`
		Href    string   `json:"href"`
		Methods []string `json:"methods"`
	} `json:"links"`
	Location       *string `json:"location"`
	Mask           *string `json:"mask"`
	MasterSubnetID string  `json:"masterSubnetId"`
	NameserverID   string  `json:"nameserverId"`
	PingSubnet     string  `json:"pingSubnet"`
	ScanAgent      *string `json:"scanAgent"`
	SectionID      string  `json:"sectionId"`
	ShowName       string  `json:"showName"`
	Subnet         *string `json:"subnet"`
	Tag            string  `json:"tag"`
	Threshold      string  `json:"threshold"`
	Usage          struct {
		UsedPercent      float32     `json:"Used_percent"`
		Freehosts        interface{} `json:"freehosts"`
		FreehostsPercent float32     `json:"freehosts_percent"`
		Maxhosts         string      `json:"maxhosts"`
		Used             interface{} `json:"used"`
	} `json:"usage"`
	VlanID *string `json:"vlanId"`
	VrfID  *string `json:"vrfId"`
}

type IPAddress struct {
	ID          string  `json:"id"`
	EditDate    *string `json:"editDate"`
	Description *string `json:"description"`
	Permissions *string `json:"permissions"`

	IP                    string        `json:"ip"`
	PTR                   string        `json:"PTR"`
	PTRIgnore             string        `json:"PTRignore"`
	DeviceID              string        `json:"deviceId"`
	ExcludePing           string        `json:"excludePing"`
	FirewallAddressObject []interface{} `json:"firewallAddressObject"`
	Hostname              *string       `json:"hostname"`
	IsGateway             *string       `json:"is_gateway"`
	LastSeen              *string       `json:"lastSeen"`
	Location              *string       `json:"location"`
	Mac                   *string       `json:"mac"`
	Note                  *string       `json:"note"`
	Owner                 *string       `json:"owner"`
	Port                  *string       `json:"port"`
	SubnetID              string        `json:"subnetId"`
	Tag                   *string       `json:"tag"`
}

type SectionsResponse struct {
	Code    int         `json:"code"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    []Section   `json:"data"`
}

type SubnetsResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    []Subnet    `json:"data"`
}

type SubnetResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    Subnet      `json:"data"`
}

type IPAddressResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    []IPAddress `json:"data"`
}

type IPAddressPatchResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    interface{} `json:"data"`
}

type IPAddressDeleteResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Success interface{} `json:"success"`
	Time    float32     `json:"time"`
	Data    interface{} `json:"data"`
}

func (a *Client) NewRequest(method, path string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(fmt.Sprintf("%sapi/%s/%s", a.url, a.appid, path))
	if err != nil {
		return nil, fmt.Errorf("error generating HTTP request URL %q: %v", fmt.Sprintf("%s/api/%s/%s", a.url, a.appid, path), err)
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if a.Token != "" {
		req.Header.Add("token", a.Token)
	}
	return req, nil
}

func (a *Client) GET(path string, output interface{}) error {
	req, err := a.NewRequest("GET", path, nil)
	if err != nil {
		return err
	}
	res, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error in HTTP request: %v", err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("error unmarshalling GET response: %v", err)
	}
	return nil
}

func (a *Client) DELETE(path string, output interface{}) error {
	req, err := a.NewRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	res, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error in HTTP request: %v", err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("error unmarshalling DELETE response: %v", err)
	}
	return nil
}

func (a *Client) POST(path string, input interface{}, output interface{}) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := a.NewRequest("POST", path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	res, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error in HTTP request: %v", err)
	}
	defer res.Body.Close()
	body, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("error unmarshalling POST response: %v", err)
	}
	return nil
}

func (a *Client) PATCH(path string, input interface{}, output interface{}) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := a.NewRequest("PATCH", path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	res, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error in HTTP request: %v", err)
	}
	defer res.Body.Close()
	body, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("error unmarshalling PATCH response: %v", err)
	}
	return nil
}

func (a *Client) authenticate() error {
	req, err := a.NewRequest("POST", "user/", nil)
	if err != nil {
		return fmt.Errorf("unable to create HTTP request: %v", err)
	}
	req.SetBasicAuth(a.username, a.password)
	res, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("error in HTTP request: %v", err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading HTTP response: %v", err)
	}
	var d AuthResponse
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("error unmarshalling login response: %v", err)
	}
	if d.Code != 200 {
		return fmt.Errorf("error in auth request: %+v", d)
	}
	a.Expires, err = time.Parse(TimeFormat, d.Data.Expires)
	if err != nil {
		return fmt.Errorf("unable to parse token expiry time: %v", err)
	}
	if d.Data.Token != "" {
		a.Token = d.Data.Token
	}
	log.Printf("Authentication completed, token expiry in %s", time.Until(a.Expires))
	return nil
}
