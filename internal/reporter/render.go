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
	// Policy impact (Section 5.6): lost replicas and running pods.
	if p := r.Policy; p != nil {
		writePolicy(&b, p)
	}
	// Consolidation (Section 5.5): removable and kept nodes, savings.
	if c := r.Consolidation; c != nil {
		writeConsolidation(&b, c)
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

// writePolicy writes the replicas lost because of the target levels, with
// the violated controls (S3), and the running pods that violate them (S1).
func writePolicy(b *strings.Builder, p *PolicyImpact) {
	if len(p.Lost) > 0 {
		b.WriteString("\nReplicas lost because of the target levels:\n")
		for _, l := range p.Lost {
			fmt.Fprintf(b, "  step %d, node %s: %s\n", l.Step, l.Node, l.Pod)
			fmt.Fprintf(b, "    %s\n", l.Reason)
		}
	}
	if len(p.Running) > 0 {
		b.WriteString("\nRunning pods that violate the target levels (rejected on their next restart):\n")
		for _, v := range p.Running {
			fmt.Fprintf(b, "  %s/%s %s: %d pods, level %s\n", v.Namespace, v.Kind, v.Name, len(v.Pods), v.Level)
			fmt.Fprintf(b, "    %s\n", v.Controls)
		}
	}
}

// writeConsolidation writes the removable nodes, the kept nodes with their
// reasons, the lower bound and the savings per node and in total (UC10–UC12).
func writeConsolidation(b *strings.Builder, c *Consolidation) {
	if len(c.Removed) > 0 {
		fmt.Fprintf(b, "\nRemovable nodes, in order: %s\n", strings.Join(c.Removed, ", "))
	} else {
		b.WriteString("\nRemovable nodes: none\n")
	}
	if len(c.Kept) > 0 {
		b.WriteString("Kept nodes:\n")
		for _, k := range c.Kept {
			fmt.Fprintf(b, "  %s: %s\n", k.Node, k.Reason)
		}
	}
	fmt.Fprintf(b, "Lower bound: at least %s needed\n", plural(c.LowerBound, "node is", "nodes are"))

	if c.Total == nil {
		if len(c.Removed) > 0 {
			b.WriteString("Savings: unknown (no estimate section in the configuration)\n")
		}
		return
	}
	if len(c.Savings) == 0 {
		return
	}
	fmt.Fprintf(b, "Savings %s:\n", period(c.Hours))
	for _, s := range c.Savings {
		fmt.Fprintf(b, "  %s (%s, %s): %s, %s\n", s.Node, orUnknown(s.InstanceType), orUnknown(s.Region),
			moneyText(s.MoneyKnown, s.Money, c.Currency), co2Text(s.CO2Known, s.EnergyKWh, s.CO2eKg))
	}
	t := c.Total
	fmt.Fprintf(b, "  total: %s, %s", moneyText(true, t.Money, c.Currency), co2Text(true, t.EnergyKWh, t.CO2eKg))
	if t.MoneyUnknown > 0 || t.CO2Unknown > 0 {
		fmt.Fprintf(b, " (money unknown for %s, CO₂ unknown for %s)",
			plural(t.MoneyUnknown, "node", "nodes"), plural(t.CO2Unknown, "node", "nodes"))
	}
	b.WriteString("\n")
}

// moneyText formats a money saving, or "money unknown".
func moneyText(known bool, money float64, currency string) string {
	if !known {
		return "money unknown"
	}
	return fmt.Sprintf("%.2f %s", money, currency)
}

// co2Text formats an energy and CO₂ saving, or "CO₂e unknown".
func co2Text(known bool, kWh, kg float64) string {
	if !known {
		return "CO₂e unknown"
	}
	return fmt.Sprintf("%.1f kWh, %.1f kg CO₂e", kWh, kg)
}

// orUnknown returns s, or "unknown" if s is empty.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
