package pipeline

import (
	"context"
	"fmt"
	"net"
)

// ValidateRBL checks the remote IP against configured RBL servers
func ValidateRBL(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if len(p.cfg.Spam.RBLServers) == 0 {
		return StatusSkipped, nil, nil
	}

	ip := ictx.RemoteIP
	if ip == nil {
		return StatusSkipped, nil, nil
	}

	reversedIP := reverseIP(ip)
	if reversedIP == "" {
		return StatusSkipped, nil, nil
	}

	hits := []string{}
	for _, server := range p.cfg.Spam.RBLServers {
		lookup := fmt.Sprintf("%s.%s", reversedIP, server)
		ips, err := net.LookupIP(lookup)
		if err == nil && len(ips) > 0 {
			hits = append(hits, server)
		}
	}

	if len(hits) > 0 {
		return StatusFail, map[string]any{"rbl_hits": hits}, nil
	}

	return StatusPass, map[string]any{"rbl_hits": hits}, nil
}

func reverseIP(ip net.IP) string {
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", ipv4[3], ipv4[2], ipv4[1], ipv4[0])
	}
	return ""
}
