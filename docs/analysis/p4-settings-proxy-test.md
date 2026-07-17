# P4 settings proxy test (#184)

- brand-list uses platform.ListAdapters plus client brands
- system-proxy/test probes target via platform.DoWithProxy (default gstatic generate_204)
- systemProxyProbeFn injectable for tests
