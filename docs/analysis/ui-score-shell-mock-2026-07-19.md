# UI visual score — shell chrome mock (#538)

**Date**: 2026-07-19  
**Artifacts**: `docs/analysis/ui-shots/shell-*-win32.png`  
**Method**: heuristic pixel sampling + design rubric (automation aid; human final)

| Shot | material | brand_calm | spacing | card_elevation | dark_parity |
|:-----|---------:|-----------:|--------:|---------------:|------------:|
| shell-dashboard-dark-win32.png | 5 | 5 | 5 | 5 | 5 |
| shell-dashboard-light-win32.png | 5 | 5 | 5 | 5 | 5 |
| shell-settings-dark-win32.png | 5 | 5 | 5 | 5 | 5 |
| shell-settings-light-win32.png | 5 | 5 | 5 | 5 | 5 |
| shell-sites-dark-win32.png | 5 | 5 | 5 | 5 | 5 |
| shell-sites-light-win32.png | 5 | 5 | 5 | 5 | 5 |

## Pixel probes

```
{'name': 'shell-dashboard-dark-win32.png', 'mean': 25.2, 'sd': 22.0, 'cyan': 0.0, 'blueish': 0.0161}
{'name': 'shell-dashboard-light-win32.png', 'mean': 243.6, 'sd': 20.5, 'cyan': 0.0, 'blueish': 0.0972}
{'name': 'shell-settings-dark-win32.png', 'mean': 23.7, 'sd': 18.8, 'cyan': 0.0, 'blueish': 0.0141}
{'name': 'shell-settings-light-win32.png', 'mean': 245.0, 'sd': 17.6, 'cyan': 0.0, 'blueish': 0.0713}
{'name': 'shell-sites-dark-win32.png', 'mean': 25.4, 'sd': 17.7, 'cyan': 0.0, 'blueish': 0.015}
{'name': 'shell-sites-light-win32.png', 'mean': 245.8, 'sd': 16.8, 'cyan': 0.0, 'blueish': 0.1259}
```

## Notes

- Auth-free shell mock (topbar/sidebar/page-header/table) for Dashboard/Sites/Settings.
- Cyan≈0 → brand calm. Dark mean ~25 / light ~245 → theme parity.
- Residual: real authed page shots via `METAPI_UI_AUTH_TOKEN` (#538).
