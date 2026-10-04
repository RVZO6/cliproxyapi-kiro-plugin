package kiro

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xiaokui-dev/cliproxyapi-kiro-plugin/internal/wire"
)

// These wire types match CPA v8's quota capability without changing this
// plugin's older SDK dependency or inference protocol.
type quotaRequest struct {
	AuthIndex      string `json:"auth_index"`
	Provider       string `json:"provider"`
	StorageJSON    []byte `json:"storage_json"`
	HostCallbackID string `json:"host_callback_id"`
}
type quotaBucket struct {
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}
type quotaGroup struct {
	DisplayName string        `json:"displayName"`
	Buckets     []quotaBucket `json:"buckets"`
}
type quotaResult struct {
	Subscription map[string]string `json:"subscription,omitempty"`
	Groups       []quotaGroup      `json:"groups"`
}
type usageMeter struct {
	CurrentUsage              *float64 `json:"currentUsage"`
	CurrentUsageWithPrecision *float64 `json:"currentUsageWithPrecision"`
	UsageLimit                *float64 `json:"usageLimit"`
	UsageLimitWithPrecision   *float64 `json:"usageLimitWithPrecision"`
	NextDateReset             float64  `json:"nextDateReset"`
}
type quotaUsageRow struct {
	usageMeter
	DisplayName       string `json:"displayName"`
	DisplayNamePlural string `json:"displayNamePlural"`
	ResourceType      string `json:"resourceType"`
	FreeTrialInfo     *struct {
		usageMeter
		Status string  `json:"freeTrialStatus"`
		Expiry float64 `json:"freeTrialExpiry"`
	} `json:"freeTrialInfo"`
	Bonuses []struct {
		usageMeter
		Name      string  `json:"bonusCode"`
		Status    string  `json:"status"`
		ExpiresAt float64 `json:"expiresAt"`
	} `json:"bonuses"`
}

func quotaDate(epoch float64) string {
	if epoch <= 0 || math.IsNaN(epoch) || math.IsInf(epoch, 0) {
		return ""
	}
	if epoch > 1e12 {
		epoch /= 1000
	}
	return time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339)
}
func meterBucket(m usageMeter, window string, reset float64) (quotaBucket, bool) {
	limit, used := m.UsageLimit, m.CurrentUsage
	if m.UsageLimitWithPrecision != nil {
		limit = m.UsageLimitWithPrecision
	}
	if m.CurrentUsageWithPrecision != nil {
		used = m.CurrentUsageWithPrecision
	}
	if limit == nil || used == nil || *limit <= 0 || *used < 0 || math.IsNaN(*limit) || math.IsNaN(*used) || math.IsInf(*limit, 0) || math.IsInf(*used, 0) {
		return quotaBucket{}, false
	}
	if m.NextDateReset > 0 {
		reset = m.NextDateReset
	}
	return quotaBucket{Window: window, RemainingFraction: math.Max(0, math.Min(1, (*limit-*used) / *limit)), ResetTime: quotaDate(reset), Description: fmt.Sprintf("%.2f / %.2f used", *used, *limit)}, true
}
func normalizeQuotaUsage(raw []byte) (quotaResult, error) {
	var data struct {
		NextDateReset    float64 `json:"nextDateReset"`
		SubscriptionInfo struct {
			Title string `json:"subscriptionTitle"`
			Type  string `json:"type"`
		} `json:"subscriptionInfo"`
		Rows []quotaUsageRow `json:"usageBreakdownList"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return quotaResult{}, fmt.Errorf("invalid quota response")
	}
	out := quotaResult{Groups: []quotaGroup{}}
	plan := data.SubscriptionInfo.Title
	if plan == "" {
		plan = data.SubscriptionInfo.Type
	}
	if plan != "" {
		out.Subscription = map[string]string{"plan": plan}
	}
	for _, row := range data.Rows {
		label := row.DisplayNamePlural
		if label == "" {
			label = row.DisplayName
		}
		if label == "" {
			label = row.ResourceType
		}
		if label == "" {
			label = "Credits"
		}
		group := quotaGroup{DisplayName: label, Buckets: []quotaBucket{}}
		if bucket, ok := meterBucket(row.usageMeter, "monthly", data.NextDateReset); ok {
			group.Buckets = append(group.Buckets, bucket)
		}
		if trial := row.FreeTrialInfo; trial != nil && strings.EqualFold(trial.Status, "ACTIVE") {
			if bucket, ok := meterBucket(trial.usageMeter, "free trial", trial.Expiry); ok {
				group.Buckets = append(group.Buckets, bucket)
			}
		}
		for _, bonus := range row.Bonuses {
			if strings.EqualFold(bonus.Status, "ACTIVE") {
				if bucket, ok := meterBucket(bonus.usageMeter, "bonus", bonus.ExpiresAt); ok {
					group.Buckets = append(group.Buckets, bucket)
				}
			}
		}
		if len(group.Buckets) > 0 {
			out.Groups = append(out.Groups, group)
		}
	}
	return out, nil
}
func fetchQuota(request []byte) ([]byte, error) {
	var req quotaRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, providerKiro) {
		return wire.Error("invalid_provider", "Kiro quota requires a Kiro credential"), nil
	}
	raw := req.StorageJSON
	if len(raw) == 0 {
		if req.AuthIndex == "" {
			return wire.Error("invalid_auth", "auth_index is required"), nil
		}
		got, err := hostAuthGetFn(req.AuthIndex)
		if err != nil {
			return wire.Error("invalid_auth", "credential unavailable"), nil
		}
		raw = got.JSON
	}
	var cred kiroCredential
	if err := json.Unmarshal(raw, &cred); err != nil || strings.TrimSpace(cred.AccessToken) == "" {
		return wire.Error("invalid_auth", "invalid Kiro credential"), nil
	}
	usage, err := fetchUsageLimits(req.HostCallbackID, cred)
	if err != nil {
		return wire.Error("quota_failed", err.Error()), nil
	}
	quota, err := normalizeQuotaUsage(usage)
	if err != nil {
		return wire.Error("quota_failed", err.Error()), nil
	}
	return wire.OK(quota)
}
