package admin

import "github.com/tokendancelab/metapi-go/routing"

func defaultRecordSiteProbeOutcome(siteID int64, status string, latencyMs float64, modelName *string, channelID *int64, errText *string) {
	routing.RecordSiteProbeOutcome(siteID, status, latencyMs, modelName, channelID, errText)
}
