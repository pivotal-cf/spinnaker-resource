/*
Copyright (C) 2018-Present Pivotal Software, Inc. All rights reserved.

This program and the accompanying materials are made available under the terms of the under the Apache License, Version 2.0 (the "License”); you may not use this file except in compliance with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.
*/
package spinnaker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/pivotal-cf/spinnaker-resource/concourse"
)

type SpinClient struct {
	sourceConfig concourse.Source
	client       *http.Client
}

func NewClient(source concourse.Source) (SpinClient, error) {

	var authClient AuthHttpClient
	if strings.EqualFold(source.AuthMethod, "ldap") {
		authClient = NewLdapAuthClient(source.LdapUsername, source.LdapPassword)
	} else if strings.EqualFold(source.AuthMethod, "x509") {
		authClient = NewX509AuthClient(source.X509Cert, source.X509Key)
	} else {
		return SpinClient{}, fmt.Errorf("auth_method must be set")
	}

	client, err := authClient.GetClient(source.SpinnakerAPI)
	if err != nil {
		return SpinClient{}, err
	}

	res, err := client.Get(fmt.Sprintf("%s/applications/%s", source.SpinnakerAPI, source.SpinnakerApplication))
	if err != nil {
		return SpinClient{}, err
	} else if res.StatusCode == 404 {
		err = fmt.Errorf("spinnaker application %s not found", source.SpinnakerApplication)
		return SpinClient{}, err
	} else if res.StatusCode >= 400 {
		body, err := ioutil.ReadAll(res.Body)
		if err == nil {
			err = fmt.Errorf("spinnaker api responded with status code: %d, body: %s", res.StatusCode, string(body))
		}
		return SpinClient{}, err
	}

	res, err = client.Get(fmt.Sprintf("%s/applications/%s/pipelineConfigs", source.SpinnakerAPI, source.SpinnakerApplication))
	if err != nil {
		return SpinClient{}, err
	} else if res.StatusCode >= 400 {
		body, err := ioutil.ReadAll(res.Body)
		if err == nil {
			err = fmt.Errorf("spinnaker api responded with status code: %d, body: %s", res.StatusCode, string(body))
			return SpinClient{}, err
		}
	} else {
		var pipelineConfigs []map[string]interface{}
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return SpinClient{}, err
		}

		err = json.Unmarshal(body, &pipelineConfigs)
		if err != nil {
			return SpinClient{}, err
		}

		found := false
		for _, pc := range pipelineConfigs {
			if pc["name"].(string) == source.SpinnakerPipeline {
				found = true
				break
			}
		}
		if !found {
			err = fmt.Errorf("spinnaker pipeline %s not found", source.SpinnakerPipeline)
			return SpinClient{}, err
		}
	}

	spinClient := SpinClient{
		sourceConfig: source,
		client:       client,
	}
	return spinClient, nil
}

func (c *SpinClient) GetPipelineExecution(pipelineExecutionID string) (map[string]interface{}, error) {
	var pipelineExecutionMetadata map[string]interface{}
	bytes, err := c.GetPipelineExecutionRaw(pipelineExecutionID)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytes, &pipelineExecutionMetadata)
	if err != nil {
		return nil, err
	}
	return pipelineExecutionMetadata, nil
}

func (c *SpinClient) GetPipelineExecutionRaw(pipelineExecutionID string) ([]byte, error) {
	url := fmt.Sprintf("%s/pipelines/%s", c.sourceConfig.SpinnakerAPI, pipelineExecutionID)
	response, err := c.client.Get(url)
	if err != nil {
		return nil, err
	} else if response.StatusCode == 404 {
		err = fmt.Errorf("pipeline execution ID not found (ID: %s)", pipelineExecutionID)
		return nil, err
	} else if response.StatusCode >= 400 {
		body, err := ioutil.ReadAll(response.Body)
		if err == nil {
			err = fmt.Errorf("spinnaker api responded with status code: %d, body: %s", response.StatusCode, string(body))
		}
		return nil, err
	}
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

//returns the last 25 spinnaker pipeline executions
func (c *SpinClient) GetPipelineExecutions() ([]PipelineExecution, error) {
	var pipelineExecutions []PipelineExecution

	//TODO What does expand do ??
	url := fmt.Sprintf("%s/applications/%s/pipelines?limit=25", c.sourceConfig.SpinnakerAPI, c.sourceConfig.SpinnakerApplication)

	if response, err := c.client.Get(url); err != nil {
		return nil, err
	} else if response.StatusCode >= 400 {
		body, err := ioutil.ReadAll(response.Body)
		if err == nil {
			err = fmt.Errorf("spinnaker api responded with status code: %d, body: %s", response.StatusCode, string(body))
		}
		return nil, err
	} else {
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(body, &pipelineExecutions)
		if err != nil {
			return nil, err
		}
		return pipelineExecutions, nil
	}
}

func (c *SpinClient) InvokePipelineExecution(body []byte) (PipelineExecution, error) {

	pipelineExecution := PipelineExecution{}

	url := fmt.Sprintf("%s/pipelines/%s/%s", c.sourceConfig.SpinnakerAPI, c.sourceConfig.SpinnakerApplication, c.sourceConfig.SpinnakerPipeline)

	if response, err := c.client.Post(url, "application/json", bytes.NewBuffer(body)); err != nil {
		return pipelineExecution, err
	} else if response.StatusCode >= 400 {
		body, err := ioutil.ReadAll(response.Body)
		if err == nil {
			err = fmt.Errorf("spinnaker api responded with status code: %d, body: %s", response.StatusCode, string(body))
		}
		return pipelineExecution, err
	} else {
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return pipelineExecution, err
		}
		var Data map[string]interface{}
		err = json.Unmarshal(body, &Data)
		if err != nil {
			return pipelineExecution, err
		}

		pipelineExecution.ID = strings.Split(Data["ref"].(string), "/")[2]
		return pipelineExecution, nil
	}
}
