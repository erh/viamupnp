// Package viamupnp is for discovering and using upnp cameras
package viamupnp

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/koron/go-ssdp"
	"go.viam.com/rdk/logging"
)

// DeviceQuery specifics a query for a network device.
type DeviceQuery struct {
	ModelName    string   `json:"model_name"`
	Manufacturer string   `json:"manufacturer"`
	SerialNumber string   `json:"serial_number"`
	Network      string   `json:"network"`
	Endpoints    []string `json:"endpoints"`
}

// UPNPDevice is a UPNPDevice device.
type UPNPDevice struct {
	Service ssdp.Service
	Desc    *deviceDesc
}

func parseNetworks(queries []DeviceQuery) []string {
	networks := []string{}
	for _, query := range queries {
		if query.Network != "" && slices.Contains(networks, query.Network) {
			networks = append(networks, query.Network)
		}
	}
	return networks
}

// FindHost looks for a host matching the query, returns just the host/ip (no port).
func FindHost(ctx context.Context, logger logging.Logger, queries []DeviceQuery, rootOnly bool) ([]string, error) {

	networks := parseNetworks(queries)
	all, err := findAll(ctx, logger, networks, rootOnly)
	if err != nil {
		return []string{}, err
	}

	hostnames := []string{}
	for _, a := range all {
		for _, query := range queries {
			if a.Matches(query) {
				u, err := url.Parse(a.Service.Location)
				if err != nil {
					// should be impossible
					logger.Warnf("invalid location %s", a.Service.Location)
					continue
				}
				if len(query.Endpoints) > 0 {
					for _, endpoint := range query.Endpoints {
						hostnames = append(hostnames, fmt.Sprintf("%s/%s", u.Hostname(), endpoint))
					}
					continue
				}

				hostnames = append(hostnames, u.Hostname())
			}
		}
	}
	if len(hostnames) > 0 {
		return hostnames, nil
	}

	return []string{}, fmt.Errorf("no match found for queries: %v", queries)
}

func matches(query string, s string) bool {
	if query == s {
		return true
	}

	if strings.HasSuffix(query, ".*") {
		query = query[0 : len(query)-2]
		return strings.HasPrefix(s, query)
	}

	return false
}

// Matches returns if the UPNPDevice matches the query.
func (pc *UPNPDevice) Matches(query DeviceQuery) bool {
	if query.ModelName != "" && !matches(query.ModelName, pc.Desc.Device.ModelName) {
		return false
	}

	if query.Manufacturer != "" && !matches(query.Manufacturer, pc.Desc.Device.Manufacturer) {
		return false
	}

	if query.SerialNumber != "" && !matches(query.SerialNumber, pc.Desc.Device.SerialNumber) {
		return false
	}

	return true
}

// FindAllTestKeyStruct - for testing.
type FindAllTestKeyStruct string

// FindAllTestKey - for testing.
const FindAllTestKey = FindAllTestKeyStruct("findAllTestKey1231231231231")

func findAll(ctx context.Context, logger logging.Logger, networks []string, rootOnly bool) ([]UPNPDevice, error) {
	all, ok := ctx.Value(FindAllTestKey).([]UPNPDevice)
	if ok {
		return all, nil
	}

	// All returns all services, which can be useful for debugging or looking for specific endpoints.
	searchType := ssdp.All
	if rootOnly {
		// RootDevice only returns the root, which significantly reduces the amount of services to test.
		searchType = ssdp.RootDevice
	}

	all = []UPNPDevice{}
	for _, network := range networks {
		list, err := ssdp.Search(searchType, 1, network) //nolint:mnd
		if err != nil {
			return nil, err
		}

		for _, srv := range list {
			logger.Debugf("found service (%s) at %s", srv.Type, srv.Location)

			desc, err := readDeviceDesc(ctx, srv.Location)
			if err != nil {
				logger.Warnf("cannot read description %v", err)
				continue
			}

			logger.Debugf("got description %v", desc)

			all = append(all, UPNPDevice{srv, desc})
		}
	}
	fmt.Println("yo all: ", len(all))

	return all, nil
}

type deviceDesc struct {
	XMLName     xml.Name `xml:"root"`
	SpecVersion struct {
		Major int `xml:"major"`
		Minor int `xml:"minor"`
	} `xml:"specVersion"`
	Device struct {
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		SerialNumber string `xml:"serialNumber"`
	} `xml:"device"`
}

func readDeviceDesc(ctx context.Context, url string) (*deviceDesc, error) {
	cli := &http.Client{
		Timeout: time.Second * 10, //nolint: mnd
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't fetch xml(%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http fetch (%s) not ok: %v", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read body from (%s): %v", url, resp.StatusCode)
	}

	return parseDeviceDesc(url, data)
}

func parseDeviceDesc(url string, data []byte) (*deviceDesc, error) {
	var desc deviceDesc
	err := xml.Unmarshal(data, &desc)
	if err != nil {
		return nil, fmt.Errorf("bad xml from (%s): %w", url, err)
	}

	return &desc, nil
}
