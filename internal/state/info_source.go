/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package state

type InfoType uint

const (
	InfoTypeClusterInfo = iota
	InfoTypeClusterPolicyCR
)

func NewInfoCatalog() InfoCatalog {
	return &infoCatalog{infoSources: make(map[InfoType]InfoSource)}
}

// InfoSource represents an object that is a souce of information
type InfoSource interface{}

// InfoCatalog is an information catalog to be used to retrieve infoSources. used for State implementation that require
// additional helping functionality to perfrom the Sync operation. As more States are added,
// more infoSources may be added to aid them. for any infoSource if not present in the catalog, nil will be returned.
type InfoCatalog interface {
	// Add an infoSource of InfoType to the catalog
	Add(InfoType, InfoSource)
	// Get an InfoSource of InfoType
	Get(InfoType) InfoSource
}

type infoCatalog struct {
	infoSources map[InfoType]InfoSource
}

func (sc *infoCatalog) Add(infoType InfoType, infoSource InfoSource) {
	sc.infoSources[infoType] = infoSource
}

func (sc *infoCatalog) Get(infoType InfoType) InfoSource {
	infoSource, ok := sc.infoSources[infoType]
	if !ok {
		return nil
	}
	return infoSource
}
