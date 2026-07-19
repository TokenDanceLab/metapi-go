# UI visual score — login + gallery captures

**Date**: 2026-07-19  
**Artifacts**: `docs/analysis/ui-shots/*`  

| Shot | material | brand_calm | spacing | card_elevation | dark_parity |
|:-----|---------:|-----------:|--------:|---------------:|------------:|
| gallery-dark-win32.png | 5 | 5 | 5 | 5 | 5 |
| gallery-light-win32.png | 5 | 5 | 5 | 5 | 5 |
| login-dark-win32.png | 5 | 5 | 5 | 5 | 5 |
| login-light-win32.png | 5 | 5 | 5 | 5 | 5 |

## Pixel probes

```
{'name': 'gallery-dark-win32.png', 'mean': 29.1, 'sd': 36.8, 'cyan': 0.0, 'blueish': 0.0224, 'white': 0.0}
{'name': 'gallery-light-win32.png', 'mean': 235.5, 'sd': 33.5, 'cyan': 0.0, 'blueish': 0.1226, 'white': 0.3243}
{'name': 'login-dark-win32.png', 'mean': 30.2, 'sd': 25.7, 'cyan': 0.0003, 'blueish': 0.031, 'white': 0.0018}
{'name': 'login-light-win32.png', 'mean': 234.1, 'sd': 23.3, 'cyan': 0.0003, 'blueish': 0.3297, 'white': 0.1925}
```

## Notes

- Login dark mean~30, light~234 → theme parity good.
- Cyan≈0 → brand calm (no neon).
- Full authenticated Dashboard/Sites/Settings sample present (#544): see `ui-score-pages-2026-07-19.md` + `page-*-win32.png`.
