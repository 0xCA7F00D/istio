//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package configz

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/mcp/client"
	"istio.io/istio/galley/pkg/mcp/snapshot"
	"istio.io/istio/galley/pkg/mcp/testing"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/ctrlz/fw"
)

type updater struct {
}

func (u *updater) Update(c *client.Change) error {
	return nil
}

func TestConfigZ(t *testing.T) {
	s, err := mcptest.NewServer(0, []string{"type.googleapis.com/google.protobuf.Empty"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	cc, err := grpc.Dial(fmt.Sprintf("localhost:%d", s.Port), grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}

	u := &updater{}
	clnt := mcp.NewAggregatedMeshConfigServiceClient(cc)
	cl := client.New(clnt, []string{"google.protobuf.Empty"}, u, "zoo", map[string]string{"foo": "bar"})

	ctx, cancel := context.WithCancel(context.Background())
	go cl.Run(ctx)
	defer cancel()

	o := ctrlz.DefaultOptions()
	go ctrlz.Run(o, []fw.Topic{CreateTopic(cl)})

	baseURL := fmt.Sprintf("http://%s:%d", o.Address, o.Port)

	t.Run("configz with no requests", func(tt *testing.T) { testConfigZWithNoRequest(tt, baseURL) })

	b := snapshot.NewInMemoryBuilder()
	b.SetVersion("type.googleapis.com/google.protobuf.Empty", "23")
	err = b.SetEntry("type.googleapis.com/google.protobuf.Empty", "foo", &types.Empty{})
	if err != nil {
		t.Fatalf("Setting an entry should not have failed: %v", err)
	}
	s.Cache.SetSnapshot("zoo", b.Build())

	t.Run("configz with 1 request", func(tt *testing.T) { testConfigZWithOneRequest(tt, baseURL) })

	t.Run("configj with 1 request", func(tt *testing.T) { testConfigJWithOneRequest(tt, baseURL) })
}

func testConfigZWithNoRequest(t *testing.T, baseURL string) {
	// First, test configz, with no recent requests.
	data := request(t, baseURL+"/configz")
	if !strings.Contains(data, "zoo") {
		t.Fatalf("Node id should have been displayed: %q", data)
	}
	if !strings.Contains(data, "foo") || !strings.Contains(data, "bar") {
		t.Fatalf("Metadata should have been displayed: %q", data)
	}
	if !strings.Contains(data, "type.googleapis.com/google.protobuf.Empty") {
		t.Fatalf("Supported urls should have been displayed: %q", data)
	}
	if strings.Count(data, "type.googleapis.com/google.protobuf.Empty") != 1 {
		t.Fatalf("Only supported urls should have been displayed: %q", data)
	}
}

func testConfigZWithOneRequest(t *testing.T, baseURL string) {
	for i := 0; i < 10; i++ {
		data := request(t, baseURL+"/configz")
		if strings.Count(data, "type.googleapis.com/google.protobuf.Empty") != 2 {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		return
	}
	t.Fatal("Both supported urls and a recent request should have been displayed")
}

func testConfigJWithOneRequest(t *testing.T, baseURL string) {
	data := request(t, baseURL+"/configj/")

	m := make(map[string]interface{})
	err := json.Unmarshal([]byte(data), &m)
	if err != nil {
		t.Fatalf("Should have unmarshalled json: %v", err)
	}

	if m["ID"] != "zoo" {
		t.Fatalf("Should have contained id: %v", data)
	}

	if m["Metadata"].(map[string]interface{})["foo"] != "bar" {
		t.Fatalf("Should have contained metadata: %v", data)
	}

	if len(m["SupportedTypeURLs"].([]interface{})) != 1 ||
		m["SupportedTypeURLs"].([]interface{})[0].(string) != "type.googleapis.com/google.protobuf.Empty" {
		t.Fatalf("Should have contained supported type urls: %v", data)
	}

	if len(m["LatestRequests"].([]interface{})) != 1 {
		t.Fatalf("There should have been a single LatestRequest entry: %v", data)
	}
}

func request(t *testing.T, url string) string {
	var e error
	for i := 1; i < 10; i++ {
		resp, err := http.Get(url)
		if err != nil {
			e = err
			time.Sleep(time.Millisecond * 100)
			continue
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			e = err
			time.Sleep(time.Millisecond * 100)
			continue
		}

		return string(body)
	}

	t.Fatalf("Unable to complete get request: url='%s', last err='%v'", url, e)
	return ""
}
