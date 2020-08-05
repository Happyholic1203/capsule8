// Copyright 2018 Capsule8, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cli

import (
	"encoding/json"
	"io/ioutil"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
)

// getSubscriptionsFromJSON takes a json blob and returns it
// marshalled as a slice of Subscription objects or an error
func getSubscipritionsFromJSON(blob []byte) (*telemetryAPI.Subscription, error) {
	subs := &telemetryAPI.Subscription{}

	err := json.Unmarshal(blob, &subs)
	if err != nil {
		return nil, err
	}

	return subs, nil
}

// getSubscriptionsFromFILE takes a JSON file name and returns its contents
// marshalled as a slice of Subscription objects or an error
func getSubscipritionsFromFile(filename string) (*telemetryAPI.Subscription, error) {
	blob, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return getSubscipritionsFromJSON(blob)
}
