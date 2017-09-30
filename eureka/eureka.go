package eureka

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gliderlabs/registrator/bridge"
)

const (
	eurekaScheme    = "eureka"
	eurekaTLSScheme = "eureka-tls"
)

const (
	defaultTimeout = 30 * time.Second
)

func init() {
	f := &factory{}
	bridge.Register(f, eurekaScheme)
	bridge.Register(f, eurekaTLSScheme)
}

type factory struct{}

func (f *factory) New(uri *url.URL) bridge.RegistryAdapter {
	u := *uri // copy to prevent modification
	switch u.Scheme {
	case eurekaScheme:
		u.Scheme = "http"
	case eurekaTLSScheme:
		u.Scheme = "https"
	}
	return &eureka{
		client: newClient(u.String(), http.Client{Timeout: defaultTimeout}),
	}
}

type eureka struct {
	client *client
}

func (e *eureka) Ping() error {
	// TODO
	return nil
}

func (e *eureka) Register(service *bridge.Service) error {
	return e.client.register(convertService(service))
}

func (e *eureka) Deregister(service *bridge.Service) error {
	inst := convertService(service)
	return e.client.deregister(inst.App, inst.ID)
}

func (e *eureka) Refresh(service *bridge.Service) error {
	inst := convertService(service)
	return e.client.heartbeat(inst.App, inst.ID)
}

func (e *eureka) Services() ([]*bridge.Service, error) {
	// TODO
	return nil, nil
}

func convertService(service *bridge.Service) *instance {
	id := strings.Replace(service.ID, ":", "-", -1) // make URL-safe
	md := make(metadata)
	md["istio.protocol"] = "http" // FIXME: hack for istio
	for _, tag := range service.Tags {
		parts := strings.SplitN(tag, "|", 2)
		key := parts[0]
		value := ""
		if len(parts) > 1 {
			value = parts[1]
		}
		md[key] = value
	}
	return &instance{
		ID:        id,
		Hostname:  service.Name,
		App:       service.Name,
		IPAddress: service.IP,
		Port: port{
			Port:    service.Port,
			Enabled: true,
		},
		Metadata: md,
	}
}

type instance struct {
	ID         string   `json:"instanceId,omitempty"`
	Hostname   string   `json:"hostName"`
	App        string   `json:"app"`
	IPAddress  string   `json:"ipAddr"`
	Port       port     `json:"port,omitempty"`
	SecurePort port     `json:"securePort,omitempty"`
	Metadata   metadata `json:"metadata,omitempty"`
}

type port struct {
	Port    int  `json:"$,string"`
	Enabled bool `json:"@enabled,string"`
}

type metadata map[string]string

const (
	appPath      = "%s/eureka/v2/apps/%s"
	instancePath = "%s/eureka/v2/apps/%s/%s"
)

type client struct {
	client http.Client
	url    string
}

func newClient(url string, cli http.Client) *client {
	return &client{
		client: cli,
		url:    url,
	}
}

func (a *client) register(inst *instance) error {
	payload := struct{Instance instance `json:"instance"`} {Instance: *inst}
	data, err := json.Marshal(&payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, a.buildRegisterPath(inst.App), bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %s", resp.Status)
	}
	return nil
}

func (a *client) heartbeat(app, id string) error {
	req, err := http.NewRequest(http.MethodPut, a.buildInstancePath(app, id), nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %s", resp.Status)
	}
	return nil
}

func (a *client) deregister(app, id string) error {
	req, err := http.NewRequest(http.MethodDelete, a.buildInstancePath(app, id), nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to unregister %s, got %s", id, resp.Status)
	}
	return nil
}

func (a *client) buildRegisterPath(app string) string {
	return fmt.Sprintf(appPath, a.url, app)
}

func (a *client) buildInstancePath(app, id string) string {
	return fmt.Sprintf(instancePath, a.url, app, id)
}