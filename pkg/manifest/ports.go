// Copyright 2025 GriffinGuard
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

package manifest

// Canonical port-type vocabulary. The port type system — not the capability-type
// string — is the currency of workflow composability: two nodes can be wired when
// their port types are compatible. This vocabulary is the authoritative set every
// interface port (standard or inline custom) must use / 权威端口类型词汇.
const (
	PortText      = "text"      // UTF-8 string
	PortInt       = "int"       // integer
	PortFloat     = "float"     // floating-point number
	PortBool      = "bool"      // boolean
	PortJSON      = "json"      // arbitrary structured object
	PortBinary    = "binary"    // inline binary blob (base64)
	PortFile      = "file"      // file path under the shared pipeline mount
	PortImage     = "image"     // image file (a file specialization)
	PortAudio     = "audio"     // audio file (a file specialization)
	PortVideo     = "video"     // video file (a file specialization)
	PortEmbedding = "embedding" // numeric vector (array of float)
	PortLLMRef    = "llm-ref"   // handle to an AI model/provider
	PortAny       = "any"       // wildcard, compatible with any type
)

// knownPortTypes is the canonical port-type vocabulary as a lookup set.
var knownPortTypes = map[string]bool{
	PortText: true, PortInt: true, PortFloat: true, PortBool: true,
	PortJSON: true, PortBinary: true, PortFile: true, PortImage: true,
	PortAudio: true, PortVideo: true, PortEmbedding: true, PortLLMRef: true,
	PortAny: true,
}

// IsValidPortType reports whether t is a member of the canonical port-type
// vocabulary / 判断 t 是否属于权威端口类型词汇.
func IsValidPortType(t string) bool {
	return knownPortTypes[t]
}
