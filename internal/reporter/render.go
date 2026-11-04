package reporter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// JSON writes the report as indented JSON (FR34). The field formatVersion
// lets tools reading it detect changes of the format (Section 5.7).
func JSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Text writes the report for people (FR33): the summary, every step and the
// cause of a failure, in the order of Section 5.7.
func Text(w io.Writer, r Report) error {
	var b strings.Builder

	// Summary.
	if r.Passed {
		b.WriteString("PASSED  ")
	} else {
		b.WriteString("FAILED  ")
	}
	b.WriteString(r.Summary + "\n")

	// How the order was found, if the given order failed and a strategy
	// searched for another one (UC3, UC5).
	if s := r.Search; s != nil && s.Strategy != "" {
		fmt.Fprintf(&b, "Search: %s strategy, %d attempts", s.Strategy, s.Attempts)
		if s.Stopped {
			b.WriteString(", stopped at the limit")
		}
		b.WriteString("\n")
	}

	// Every simulated step.
	for _, s := range r.Steps {
		state := "drained"
		if !s.Drained {
			state = "not drained"
		}
		fmt.Fprintf(&b, "\nStep %d: %s, %s\n", s.Number, s.Node, state)
		for _, p := range s.Evicted {
			fmt.Fprintf(&b, "  evicted  %s\n", p)
		}
		for _, rp := range s.Replacements {
			writeReplacement(&b, rp)
		}
		for _, p := range s.Skipped {
			fmt.Fprintf(&b, "  skipped  %s (%s)\n", p.Pod, p.Reason)
		}
		for _, p := range s.Refused {
			fmt.Fprintf(&b, "  refused  %s (%s)\n", p.Pod, p.Reason)
		}
		for _, p := range s.Blocked {
			fmt.Fprintf(&b, "  blocked  %s (%s)\n", p.Pod, p.Reason)
		}
	}

	// Cause of the failure (FR18).
	if f := r.Failure; f != nil {
		fmt.Fprintf(&b, "\nCause of the failure (step %d, node %s):\n", f.Step, f.Node)
		switch f.Cause {
		case "budget":
			b.WriteString("  These pods cannot be evicted, because their PodDisruptionBudget allows no further disruption:\n")
		case "refused":
			b.WriteString("  kubectl drain refuses these pods without --force or --delete-emptydir-data:\n")
		}
		for _, p := range f.Pods {
			fmt.Fprintf(&b, "    %s: %s\n", p.Pod, p.Reason)
		}
		if len(f.Unplaced) > 0 {
			b.WriteString("  The budget did not recover, because these replacements were not placed:\n")
			for _, rp := range f.Unplaced {
				fmt.Fprintf(&b, "    %s (replaces %s), %s\n", rp.Pod, rp.Evicted, rp.Outcome)
				writeReasons(&b, rp.Reason, "      ")
			}
		}
	}

	// Snapshot the simulation ran on.
	writeSnapshot(&b, r.Snapshot)

	_, err := io.WriteString(w, b.String())
	return err
}

// writeReplacement writes one line about a replacement and, if it was not
// placed, the reasons below it.
func writeReplacement(b *strings.Builder, rp Replacement) {
	switch rp.Outcome {
	case "placed":
		fmt.Fprintf(b, "  placed   %s on %s (replaces %s)\n", rp.Pod, rp.Node, rp.Evicted)
	case "pending":
		fmt.Fprintf(b, "  pending  %s (replaces %s), no node fits:\n", rp.Pod, rp.Evicted)
		writeReasons(b, rp.Reason, "           ")
	case "lost":
		fmt.Fprintf(b, "  lost     %s (replaces %s), rejected by Pod Security:\n", rp.Pod, rp.Evicted)
		writeReasons(b, rp.Reason, "           ")
	}
}

// writeSnapshot writes when the snapshot was taken and the pods for which the
// result may differ from a real drain (Section 5.7).
func writeSnapshot(b *strings.Builder, s Snapshot) {
	fmt.Fprintf(b, "\nSnapshot taken %s (%s ago), Kubernetes %s\n",
		s.CreatedAt.UTC().Format(time.RFC3339), s.Age, s.KubernetesVersion)
	for _, w := range s.Warnings {
		fmt.Fprintf(b, "  warning: %s: %s\n", w.Topic, w.Message)
		for _, p := range w.Pods {
			fmt.Fprintf(b, "    %s\n", p)
		}
	}
}

// writeReasons writes a reason with one line per rejected node. The engine
// joins the rejections of the scheduler with "; " (FR19).
func writeReasons(b *strings.Builder, reason, indent string) {
	for _, part := range strings.Split(reason, "; ") {
		if part != "" {
			b.WriteString(indent + part + "\n")
		}
	}
}
