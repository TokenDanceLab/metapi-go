# Usage heatmaps and slow-request ranking (#121)

## APIs
- 
  - cells:  hard 
  - site dimension prefers  then falls back to 
  - model dimension aggregates  by hour prefix + model
- 
  - top  by  with bounds: limit<=200, hours<=168

## Guarantees
- No unbounded full-table scans
- No chat content in responses
