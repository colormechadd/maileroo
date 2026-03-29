package pipeline

import (
	"bytes"
	"context"
	"strings"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-msgauth/dmarc"
	"github.com/zaccone/spf"
)

// ValidateSender performs SPF, DKIM and optionally DMARC checks.
// It requires either SPF or DKIM to pass.
func ValidateSender(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	domain := extractDomain(ictx.FromAddress)

	// 1. SPF Check
	spfRes, spfExp, spfErr := spf.CheckHost(ictx.RemoteIP, domain, ictx.FromAddress)
	spfPass := (spfRes == spf.Pass)

	// 2. DKIM Check
	dkimStatus, dkimResults, _ := checkDKIM(ictx.RawMessage)
	dkimPass := (dkimStatus == StatusPass)

	results := map[string]any{
		"spf": map[string]any{
			"result":      spfRes.String(),
			"explanation": spfExp,
			"error":       spfErr,
		},
		"dkim": dkimResults,
	}

	if spfPass || dkimPass {
		return StatusPass, results, nil
	}

	// 3. DMARC Check (only if both failed)
	dmarcRecord, dmarcErr := dmarc.Lookup(domain)
	if dmarcErr == nil && dmarcRecord != nil {
		results["dmarc"] = map[string]any{
			"policy": string(dmarcRecord.Policy),
			"status": "found",
		}

		if dmarcRecord.Policy == dmarc.PolicyNone {
			return StatusPass, results, nil
		}

		if dmarcRecord.Policy == dmarc.PolicyReject || dmarcRecord.Policy == dmarc.PolicyQuarantine {
			return StatusFail, results, nil
		}
	} else {
		results["dmarc"] = map[string]any{
			"status": "not_found",
			"error":  dmarcErr,
		}
	}

	return StatusFail, results, nil
}

func checkDKIM(raw []byte) (StepStatus, []any, error) {
	r := bytes.NewReader(raw)
	verifications, err := dkim.Verify(r)
	if err != nil {
		return StatusError, nil, err
	}

	status := StatusNone
	results := []any{}
	for _, v := range verifications {
		vErr := v.Err
		vStatus := "pass"
		if vErr != nil {
			vStatus = "fail"
			status = StatusFail
		} else if status != StatusFail {
			status = StatusPass
		}
		results = append(results, map[string]any{
			"domain": v.Domain,
			"status": vStatus,
			"error":  vErr,
		})
	}
	return status, results, nil
}

func extractDomain(address string) string {
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
