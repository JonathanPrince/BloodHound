// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
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
//
// SPDX-License-Identifier: Apache-2.0

package racfhound

import "github.com/specterops/dawgs/graph"

// pathfindingRelationshipNames are the RACF edges that carry control, and so may
// appear in a path. Together with nonPathfindingRelationshipNames this must cover
// every edge kind the exporter emits — see the RACFHound repo's
// docs/graph-model.md. TestEveryEmittedEdgeIsClassified guards the split.
var pathfindingRelationshipNames = []string{
	// Access. Reaching a profile with any of these is a capability over whatever
	// the profile protects.
	"RACFCanRead",
	"RACFCanWrite",
	"RACFCanExecute",
	"RACFCanIssue",

	// Structure and ownership. Ownership matters because owning a profile permits
	// changing its ACL; connect authority above USE permits managing the group.
	"RACFMemberOf",
	"RACFOwns",
	"RACFClassAuth",
	"RACFGroupAuth_USE",
	"RACFGroupAuth_CREATE",
	"RACFGroupAuth_CONNECT",
	"RACFGroupAuth_JOIN",
	"RACFGroupScopeSpecial",
	"RACFGroupScopeOper",

	// Derived. These are the edges most attack-path queries actually traverse.
	"RACFHasPrivilege",
	"RACFSurrogateFor",
	"RACFPassticketFor",
	"RACFCanAccessKey",
	"RACFLoadsFrom",
	"RACFControlsTask",
	"RACFStartedTaskRunsAs",
	"RACFStartedTaskGroup",
	"RACFCertificateFor",
}

// nonPathfindingRelationshipNames are RACF edges that describe or annotate the
// environment rather than conferring control. Excluding them keeps Pathfinder
// from proposing paths that are not privilege escalation.
var nonPathfindingRelationshipNames = []string{
	// Group-SPECIAL administrative scope, not ordinary group membership: reaching
	// a subgroup this way does not grant that subgroup's permissions.
	"RACFHasSubgroup",

	// A revoked connection is the absence of access.
	"RACFGroupRevoke",

	// Descriptive: which generic profile controls a concrete data set. The
	// exporter already propagates the access edge itself to the data set with
	// via=via_generic_profile, so traversing this as well would double-count.
	"RACFGenericCovers",

	// An MFA factor is an attribute of a user, not something a user can use to
	// reach anything else.
	"RACFHasMFAFactor",
}

var nonPathfindingRelationshipKinds = stringKinds(nonPathfindingRelationshipNames)

func stringKinds(names []string) graph.Kinds {
	kinds := make(graph.Kinds, 0, len(names))

	for _, name := range names {
		kinds = append(kinds, graph.StringKind(name))
	}

	return kinds
}

// PathfindingRelationships returns the control relationships Pathfinder may
// traverse.
func PathfindingRelationships() graph.Kinds {
	return stringKinds(pathfindingRelationshipNames)
}

// IsNonPathfindingRelationship identifies RACF edges that describe or annotate
// the environment — Group-SPECIAL scope, revoked connections, generic-profile
// coverage, MFA factors — rather than conferring control.
func IsNonPathfindingRelationship(kind graph.Kind) bool {
	return nonPathfindingRelationshipKinds.ContainsOneOf(kind)
}
