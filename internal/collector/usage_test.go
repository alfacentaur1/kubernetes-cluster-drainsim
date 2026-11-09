package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// cpuResponse is a recorded answer to the CPU query (values in cores).
// Two pods of the Deployment web (through ReplicaSet web-7d4b9) report
// different usage; the workload must get the larger one. A NaN sample, as
// quantile_over_time returns for a series without enough points, is skipped.
const cpuResponse = `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"namespace":"shop","pod":"web-7d4b9-aaaaa","owner_kind":"ReplicaSet","owner_name":"web-7d4b9","container":"app"},"value":[1767323045,"0.12"]},
 {"metric":{"namespace":"shop","pod":"web-7d4b9-bbbbb","owner_kind":"ReplicaSet","owner_name":"web-7d4b9","container":"app"},"value":[1767323045,"0.2501"]},
 {"metric":{"namespace":"shop","pod":"legacy-xyz12","owner_kind":"ReplicaSet","owner_name":"legacy","container":"app"},"value":[1767323045,"0.05"]},
 {"metric":{"namespace":"shop","pod":"debug","owner_kind":"<none>","owner_name":"<none>","container":"shell"},"value":[1767323045,"0.001"]},
 {"metric":{"namespace":"db","pod":"pg-0","owner_kind":"StatefulSet","owner_name":"pg","container":"postgres"},"value":[1767323045,"1.5"]},
 {"metric":{"namespace":"db","pod":"pg-0","owner_kind":"StatefulSet","owner_name":"pg","container":"exporter"},"value":[1767323045,"NaN"]}
]}}`

// memoryResponse is a recorded answer to the memory query (values in bytes).
const memoryResponse = `{"status":"success","data":{"resultType":"vector","result":[
 {"metric":{"namespace":"shop","pod":"web-7d4b9-aaaaa","owner_kind":"ReplicaSet","owner_name":"web-7d4b9","container":"app"},"value":[1767323045,"157286400"]},
 {"metric":{"namespace":"shop","pod":"web-7d4b9-bbbbb","owner_kind":"ReplicaSet","owner_name":"web-7d4b9","container":"app"},"value":[1767323045,"104857600"]},
 {"metric":{"namespace":"shop","pod":"legacy-xyz12","owner_kind":"ReplicaSet","owner_name":"legacy","container":"app"},"value":[1767323045,"1000.5"]},
 {"metric":{"namespace":"shop","pod":"debug","owner_kind":"<none>","owner_name":"<none>","container":"shell"},"value":[1767323045,"2048"]},
 {"metric":{"namespace":"db","pod":"pg-0","owner_kind":"StatefulSet","owner_name":"pg","container":"postgres"},"value":[1767323045,"536870912"]},
 {"metric":{"namespace":"db","pod":"pg-0","owner_kind":"StatefulSet","owner_name":"pg","container":"exporter"},"value":[1767323045,"20971520"]}
]}}`

// fakePrometheus serves the recorded responses and records the queries.
func fakePrometheus(t *testing.T, queries *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/query" {
			t.Errorf("request %s %s, want GET /api/v1/query", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("query")
		*queries = append(*queries, q)
		if r.URL.Query().Get("time") == "" {
			t.Errorf("query without time parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(q, "container_cpu_usage_seconds_total") {
			w.Write([]byte(cpuResponse))
		} else {
			w.Write([]byte(memoryResponse))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// replicaSets returns the ReplicaSet web-7d4b9 owned by the Deployment web
// and the ReplicaSet legacy that nothing owns.
func replicaSets() []appsv1.ReplicaSet {
	yes := true
	return []appsv1.ReplicaSet{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "web-7d4b9", OwnerReferences: []metav1.OwnerReference{
			{APIVersion: "apps/v1", Kind: "Deployment", Name: "web", Controller: &yes},
		}}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "legacy"}},
	}
}

// TestCollectReadsUsageProfiles checks FR20: usage is assigned to workloads,
// a ReplicaSet is resolved to its Deployment, and the maximum over the pods
// of a workload is taken.
func TestCollectReadsUsageProfiles(t *testing.T) {
	var queries []string
	srv := fakePrometheus(t, &queries)

	rs := replicaSets()
	client := fake.NewClientset(&rs[0], &rs[1])
	c := newWithClient(client, Options{PrometheusURL: srv.URL, UsageWindow: 7 * 24 * time.Hour})

	snap, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	want := []snapshot.UsageProfile{
		// pg-0: exporter has no CPU sample (NaN), only memory.
		{Namespace: "db", Kind: "StatefulSet", Name: "pg", Window: "168h", Containers: []snapshot.ContainerUsage{
			{Name: "exporter", CPUP95Millicores: 0, MemoryMaxBytes: 20971520},
			{Name: "postgres", CPUP95Millicores: 1500, MemoryMaxBytes: 536870912},
		}},
		// max(0.12, 0.2501) cores = 250.1m, rounded up to 251m;
		// max(150 MiB, 100 MiB) = 157286400 B.
		{Namespace: "shop", Kind: "Deployment", Name: "web", Window: "168h", Containers: []snapshot.ContainerUsage{
			{Name: "app", CPUP95Millicores: 251, MemoryMaxBytes: 157286400},
		}},
		// A pod without an owner is its own workload.
		{Namespace: "shop", Kind: "Pod", Name: "debug", Window: "168h", Containers: []snapshot.ContainerUsage{
			{Name: "shell", CPUP95Millicores: 1, MemoryMaxBytes: 2048},
		}},
		// A ReplicaSet without a Deployment stays a ReplicaSet; 1000.5 B rounds up.
		{Namespace: "shop", Kind: "ReplicaSet", Name: "legacy", Window: "168h", Containers: []snapshot.ContainerUsage{
			{Name: "app", CPUP95Millicores: 50, MemoryMaxBytes: 1001},
		}},
	}
	if !reflect.DeepEqual(snap.UsageProfiles, want) {
		t.Errorf("UsageProfiles =\n%+v\nwant\n%+v", snap.UsageProfiles, want)
	}

	if len(queries) != 2 {
		t.Fatalf("got %d queries, want 2", len(queries))
	}
	for _, q := range queries {
		if !strings.Contains(q, "[168h") || !strings.Contains(q, "max_over_time(kube_pod_owner") {
			t.Errorf("query %q does not use the window 168h and the owners of the window", q)
		}
	}
}

// TestCollectWithoutPrometheus checks that no profiles are collected without
// a Prometheus URL (Section 5.5: consolidation works without them).
func TestCollectWithoutPrometheus(t *testing.T) {
	snap, err := newWithClient(fake.NewClientset(), Options{}).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snap.UsageProfiles != nil {
		t.Errorf("UsageProfiles = %+v, want nil", snap.UsageProfiles)
	}
}

// TestCollectFailsOnPrometheusError checks that an error of Prometheus is an
// error of the snapshot, so no snapshot without the requested usage is written.
func TestCollectFailsOnPrometheusError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantMsg string
	}{
		{"query error", http.StatusBadRequest,
			`{"status":"error","errorType":"bad_data","error":"parse error"}`, "bad_data: parse error"},
		{"not JSON", http.StatusBadGateway, `<html>bad gateway</html>`, "HTTP 502"},
		{"not a vector", http.StatusOK,
			`{"status":"success","data":{"resultType":"matrix","result":[]}}`, `result type "matrix"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := newWithClient(fake.NewClientset(), Options{PrometheusURL: srv.URL})
			snap, err := c.Collect(context.Background())
			if !errors.Is(err, ErrPrometheus) {
				t.Fatalf("Collect() error = %v, want ErrPrometheus", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err, tt.wantMsg)
			}
			if snap != nil {
				t.Errorf("Collect() returned a partial snapshot")
			}
			if calls.Load() != 1 {
				t.Errorf("Prometheus called %d times, want 1 (stop at the first error)", calls.Load())
			}
		})
	}
}

func TestParseWindow(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"168h", 168 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"5m", 5 * time.Minute, false},
		{"4m", 0, true},
		{"0d", 0, true},
		{"-1d", 0, true},
		{"1.5d", 0, true},
		{"week", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseWindow(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseWindow(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if err != nil && !errors.Is(err, ErrInvalidWindow) {
			t.Errorf("ParseWindow(%q) error = %v, want ErrInvalidWindow", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseWindow(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatWindow(t *testing.T) {
	tests := map[time.Duration]string{
		7 * 24 * time.Hour: "168h",
		90 * time.Minute:   "90m",
		90 * time.Second:   "90s",
	}
	for d, want := range tests {
		if got := formatWindow(d); got != want {
			t.Errorf("formatWindow(%v) = %q, want %q", d, got, want)
		}
	}
}
