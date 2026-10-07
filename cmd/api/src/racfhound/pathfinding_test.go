// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package racfhound_test

import (
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/racfhound"
	"github.com/specterops/dawgs/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emittedEdgeKinds is every edge kind the RACFHound exporter produces
// (mfpandas_racfhound/edges.py; documented in the RACFHound repo's
// docs/graph-model.md). RACFGroupAuth_ is built by appending the GPMEM_AUTH
// value, which RACF constrains to these four.
//
// Every one of these must be classified as either pathfinding or
// non-pathfinding. Add a new edge to the exporter, add it here and to one of the
// two lists in pathfinding.go — otherwise Pathfinder silently ignores it.
var emittedEdgeKinds = []string{
	"RACFCanAccessKey",
	"RACFCanExecute",
	"RACFCanIssue",
	"RACFCanRead",
	"RACFCanWrite",
	"RACFCertificateFor",
	"RACFClassAuth",
	"RACFControlsTask",
	"RACFGenericCovers",
	"RACFGroupAuth_CONNECT",
	"RACFGroupAuth_CREATE",
	"RACFGroupAuth_JOIN",
	"RACFGroupAuth_USE",
	"RACFGroupRevoke",
	"RACFGroupScopeOper",
	"RACFGroupScopeSpecial",
	"RACFHasMFAFactor",
	"RACFHasPrivilege",
	"RACFHasSubgroup",
	"RACFLoadsFrom",
	"RACFMemberOf",
	"RACFOwns",
	"RACFPassticketFor",
	"RACFStartedTaskGroup",
	"RACFStartedTaskRunsAs",
	"RACFSurrogateFor",
}

func TestEveryEmittedEdgeIsClassified(t *testing.T) {
	pathfinding := racfhound.PathfindingRelationships()

	for _, name := range emittedEdgeKinds {
		kind := graph.StringKind(name)

		traversable := pathfinding.ContainsOneOf(kind)
		excluded := racfhound.IsNonPathfindingRelationship(kind)

		assert.Truef(t, traversable || excluded, "%s is emitted by the exporter but classified in neither list", name)
		assert.Falsef(t, traversable && excluded, "%s is in both the pathfinding and non-pathfinding lists", name)
	}
}

func TestPathfindingRelationshipsAreAllEmitted(t *testing.T) {
	// Guards the other direction: a name here that the exporter never produces is
	// dead weight, and usually means the model moved on without this file.
	emitted := make(map[string]bool, len(emittedEdgeKinds))
	for _, name := range emittedEdgeKinds {
		emitted[name] = true
	}

	for _, kind := range racfhound.PathfindingRelationships() {
		assert.Truef(t, emitted[kind.String()], "%s is allowed for pathfinding but the exporter never emits it", kind.String())
	}
}

func TestPathfindingRelationships(t *testing.T) {
	relationshipKinds := racfhound.PathfindingRelationships()

	require.True(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFMemberOf")))
	require.True(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFCanWrite")))

	// The kinds added when PROGRAM and OPERCMDS profiles were promoted to their
	// own node kinds — program-control and operator-command escalation paths.
	require.True(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFCanIssue")))
	require.True(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFLoadsFrom")))
	require.True(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFControlsTask")))

	require.False(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFHasSubgroup")))
	require.False(t, relationshipKinds.ContainsOneOf(graph.StringKind("RACFEvidencedBy")))
}

func TestIsNonPathfindingRelationship(t *testing.T) {
	require.True(t, racfhound.IsNonPathfindingRelationship(graph.StringKind("RACFHasSubgroup")))
	require.True(t, racfhound.IsNonPathfindingRelationship(graph.StringKind("RACFGroupRevoke")))
	require.True(t, racfhound.IsNonPathfindingRelationship(graph.StringKind("RACFGenericCovers")))
	require.False(t, racfhound.IsNonPathfindingRelationship(graph.StringKind("RACFMemberOf")))
}
