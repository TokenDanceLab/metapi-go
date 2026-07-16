// React 19 + react-test-renderer require an act-enabled environment.
// Without this, RTR trees unmount before assertions ("Can't access .root on unmounted test renderer").
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
