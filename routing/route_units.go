package routing

import (
	"sort"

	"github.com/tokendancelab/metapi-go/store"
)

// IsOAuthRouteUnitCandidate checks if a channel references an OAuth route unit.
func IsOAuthRouteUnitCandidate(candidate RouteChannelCandidate) bool {
	return candidate.RouteUnit != nil || (candidate.Channel.OAuthRouteUnitID != nil && *candidate.Channel.OAuthRouteUnitID > 0)
}

// SelectRouteUnitMember selects a member from an OAuth route unit channel.
// Implements both round_robin and stick_until_unavailable strategies.
func SelectRouteUnitMember(
	outerCandidate RouteChannelCandidate,
	requestedModel string,
	nowISO string,
	nowMs int64,
	configuredMaxSec int,
	excludeChannelIDs []int64,
) *OAuthRouteUnitMemberCandidate {
	if !IsOAuthRouteUnitCandidate(outerCandidate) {
		return nil
	}

	eligibleMembers := getEligibleRouteUnitMembers(outerCandidate, requestedModel, nowISO)
	if len(eligibleMembers) == 0 {
		return nil
	}

	// If outer channel ID is in excludeChannelIDs, this is a failover — filter to healthy members only
	isFailover := false
	for _, id := range excludeChannelIDs {
		if id == outerCandidate.Channel.ID {
			isFailover = true
			break
		}
	}

	var candidateMembers []OAuthRouteUnitMemberCandidate
	if isFailover {
		for _, m := range eligibleMembers {
			if !IsChannelRecentlyFailed(&m.Member.FailCount, m.Member.LastFailAt, nowMs, configuredMaxSec) {
				candidateMembers = append(candidateMembers, m)
			}
		}
	} else {
		candidateMembers = filterRecentlyFailedMembers(eligibleMembers, nowMs, configuredMaxSec)
	}

	if len(candidateMembers) == 0 {
		if isFailover {
			return nil
		}
		candidateMembers = eligibleMembers
	}

	// Determine strategy
	strategy := "round_robin"
	if outerCandidate.RouteUnit != nil {
		strategy = outerCandidate.RouteUnit.Strategy
	}

	if strategy == "stick_until_unavailable" {
		member := getStickyPreferredRouteUnitMember(candidateMembers)
		if member != nil {
			return member
		}
		ordered := getRoundRobinRouteUnitMembers(candidateMembers)
		if len(ordered) > 0 {
			return &ordered[0]
		}
		return nil
	}

	// Default: round_robin
	ordered := getRoundRobinRouteUnitMembers(candidateMembers)
	if len(ordered) > 0 {
		return &ordered[0]
	}
	return nil
}

func getEligibleRouteUnitMembers(
	outerCandidate RouteChannelCandidate,
	requestedModel string,
	nowISO string,
) []OAuthRouteUnitMemberCandidate {
	if !IsOAuthRouteUnitCandidate(outerCandidate) {
		return nil
	}

	var eligible []OAuthRouteUnitMemberCandidate
	for _, m := range outerCandidate.RouteUnitMembers {
		if m.Account.Status != "active" {
			continue
		}
		if m.Site.Status == "disabled" {
			continue
		}
		if IsOAuthRouteUnitMemberCoolingDown(m.Member.CooldownUntil, nowISO) {
			continue
		}
		// Token check: account accessToken or apiToken must be non-empty
		if m.Account.AccessToken == "" && (m.Account.APIToken == nil || *m.Account.APIToken == "") {
			continue
		}
		eligible = append(eligible, m)
	}
	return eligible
}

func getRoundRobinRouteUnitMembers(members []OAuthRouteUnitMemberCandidate) []OAuthRouteUnitMemberCandidate {
	sorted := make([]OAuthRouteUnitMemberCandidate, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		lo := left.Member.LastSelectedAt
		if lo == nil {
			lo = left.Member.LastUsedAt
		}
		ro := right.Member.LastSelectedAt
		if ro == nil {
			ro = right.Member.LastUsedAt
		}
		cmp := CompareNullableTimeAsc(lo, ro)
		if cmp != 0 {
			return cmp < 0
		}
		cmp = CompareNullableTimeAsc(left.Member.LastUsedAt, right.Member.LastUsedAt)
		if cmp != 0 {
			return cmp < 0
		}
		so := left.Member.SortOrder - right.Member.SortOrder
		if so != 0 {
			return so < 0
		}
		return left.Account.ID < right.Account.ID
	})
	return sorted
}

func getStickyPreferredRouteUnitMember(members []OAuthRouteUnitMemberCandidate) *OAuthRouteUnitMemberCandidate {
	if len(members) == 0 {
		return nil
	}
	sorted := make([]OAuthRouteUnitMemberCandidate, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		lo := left.Member.LastSelectedAt
		if lo == nil {
			lo = left.Member.LastUsedAt
		}
		ro := right.Member.LastSelectedAt
		if ro == nil {
			ro = right.Member.LastUsedAt
		}
		cmp := CompareNullableTimeDesc(lo, ro)
		if cmp != 0 {
			return cmp < 0
		}
		so := left.Member.SortOrder - right.Member.SortOrder
		if so != 0 {
			return so < 0
		}
		return left.Account.ID < right.Account.ID
	})
	return &sorted[0]
}

func filterRecentlyFailedMembers(
	members []OAuthRouteUnitMemberCandidate,
	nowMs int64,
	configuredMaxSec int,
) []OAuthRouteUnitMemberCandidate {
	if len(members) <= 1 {
		return members
	}
	var healthy []OAuthRouteUnitMemberCandidate
	for _, m := range members {
		if !IsChannelRecentlyFailed(&m.Member.FailCount, m.Member.LastFailAt, nowMs, configuredMaxSec) {
			healthy = append(healthy, m)
		}
	}
	if len(healthy) > 0 {
		return healthy
	}
	return members
}

// ResolveRouteUnitMemberTokenValue extracts the token value from a member.
func ResolveRouteUnitMemberTokenValue(account store.Account) string {
	if account.AccessToken != "" {
		return account.AccessToken
	}
	if account.APIToken != nil && *account.APIToken != "" {
		return *account.APIToken
	}
	return ""
}

// IsExplicitTokenChannel checks if a channel is associated with a specific account token.
func IsExplicitTokenChannel(candidate RouteChannelCandidate) bool {
	return candidate.Channel.TokenID != nil && *candidate.Channel.TokenID > 0
}
