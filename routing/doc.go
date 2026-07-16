// Package routing implements the token-based channel selection engine with Fibonacci
// backoff cooldown and pluggable routing strategies (weighted default, round_robin,
// stable_first, least_busy, lowest_latency, lowest_cost).
package routing
