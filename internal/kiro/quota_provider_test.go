package kiro

import (
	"encoding/json"
	"github.com/xiaokui-dev/cliproxyapi-kiro-plugin/internal/hostapi"
	"strings"
	"testing"
)

func TestQuotaNormalizePrecisionAndPools(t *testing.T) {
	q, err := normalizeQuotaUsage([]byte(`{"nextDateReset":1800000000,"subscriptionInfo":{"subscriptionTitle":"Pro"},"usageBreakdownList":[{"displayName":"Credits","currentUsage":9,"currentUsageWithPrecision":2.5,"usageLimit":10,"usageLimitWithPrecision":20,"freeTrialInfo":{"freeTrialStatus":"ACTIVE","currentUsage":5,"usageLimit":10,"freeTrialExpiry":1800000000},"bonuses":[{"status":"EXPIRED","currentUsage":0,"usageLimit":100},{"status":"ACTIVE","currentUsage":1,"usageLimit":5,"expiresAt":1800000000000}]}]}`))
	if err != nil || len(q.Groups) != 1 || len(q.Groups[0].Buckets) != 3 {
		t.Fatalf("quota: %+v %v", q, err)
	}
	b := q.Groups[0].Buckets
	if b[0].RemainingFraction != .875 || b[1].RemainingFraction != .5 || b[2].RemainingFraction != .8 || b[0].ResetTime != b[2].ResetTime {
		t.Fatalf("buckets: %+v", b)
	}
	if q.Subscription["plan"] != "Pro" {
		t.Fatal(q.Subscription)
	}
}
func TestQuotaUnknownUsageNotReportedAsFull(t *testing.T) {
	q, _ := normalizeQuotaUsage([]byte(`{"usageBreakdownList":[{"usageLimit":10},{"usageLimit":0,"currentUsage":0},{"usageLimit":10,"currentUsage":20}]}`))
	if len(q.Groups) != 1 || q.Groups[0].Buckets[0].RemainingFraction != 0 {
		t.Fatal(q)
	}
}
func TestQuotaCapabilityFetchThroughHost(t *testing.T) {
	stubAuthSeams(t, nil, nil, func(req hostapi.HTTPRequest) (*hostapi.HTTPResponse, error) {
		if req.HostCallbackID != "cb-quota" || !strings.Contains(req.URL, "getUsageLimits") {
			t.Fatal(req.URL)
		}
		return &hostapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"usageBreakdownList":[{"currentUsage":1,"usageLimit":10}]}`)}, nil
	})
	raw, _ := json.Marshal(quotaRequest{Provider: "kiro", StorageJSON: []byte(`{"accessToken":"synthetic","region":"us-east-1"}`), HostCallbackID: "cb-quota"})
	out, err := HandleMethod("quota.fetch", raw)
	if err != nil || !strings.Contains(string(out), `"remainingFraction":0.9`) {
		t.Fatalf("%s %v", out, err)
	}
	if !kiroRegistration().Capabilities.QuotaProvider {
		t.Fatal("missing quota capability")
	}
}
