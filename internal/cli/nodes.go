package cli

import (
	"fmt"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/state"
	"k8s.io/apimachinery/pkg/labels"
)

// selectNodes decides which nodes the drain command simulates, and in which
// order (FR5). The user has three ways to choose:
//
//	drainsim drain cluster.json                       -> all nodes, sorted by name
//	drainsim drain cluster.json --nodes c,a           -> exactly c, then a
//	drainsim drain cluster.json --selector pool=blue  -> nodes labelled pool=blue, sorted by name
//
// names holds the values of --nodes and selector the value of --selector.
// Cobra already refuses both flags at once, so at most one of them is set.
// The returned order matters: the drain is simulated node by node in it.
func selectNodes(st *state.ClusterState, names []string, selector string) ([]string, error) {
	switch {

	// Case 1: --nodes was given. The user chose the nodes and their order,
	// so the list is returned as it is, after two checks.
	case len(names) > 0:
		// seen remembers the names already checked, to find duplicates.
		seen := map[string]bool{}
		for _, n := range names {
			// A name that is not in the snapshot is most likely a typo.
			// Stop with an error instead of silently ignoring it.
			if st.Node(n) == nil {
				return nil, fmt.Errorf("node %q is not in the snapshot", n)
			}
			// The same node twice would be drained twice, which makes no
			// sense, so it is an error too.
			if seen[n] {
				return nil, fmt.Errorf("node %q is given twice", n)
			}
			seen[n] = true
		}
		// Keep the order the user wrote.
		return names, nil

	// Case 2: --selector was given. The nodes are chosen by their labels,
	// in the same way as kubectl get nodes -l pool=blue.
	case selector != "":
		// Parse turns the text "pool=blue" into a selector that can test
		// labels. It also understands "pool!=blue", "pool in (blue,green)"
		// and several conditions joined by commas.
		sel, err := labels.Parse(selector)
		if err != nil {
			return nil, fmt.Errorf("invalid selector %q: %w", selector, err)
		}

		var out []string
		// st.Nodes() returns the nodes sorted by name, so the result is
		// sorted by name as well (NFR7).
		for _, n := range st.Nodes() {
			// labels.Set is the map of labels in the form Matches expects.
			if sel.Matches(labels.Set(n.Labels)) {
				out = append(out, n.Name)
			}
		}
		// A selector that matches nothing is most likely a mistake; an empty
		// drain would "pass" and hide it.
		if len(out) == 0 {
			return nil, fmt.Errorf("no node matches the selector %q", selector)
		}
		return out, nil

	// Case 3: neither flag was given. Drain every node of the snapshot,
	// sorted by name, so that the same snapshot always gives the same order.
	default:
		return st.NodeNames(), nil
	}
}
