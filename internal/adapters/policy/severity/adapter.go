// Package severity normalizes bounded tool-native severity labels without assigning governance.
package severity

import (
	"fmt"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }
func (Adapter) Normalize(native string) (policy.Severity, error) {
	switch strings.ToUpper(strings.TrimSpace(native)) {
	case "INFO", "INFORMATIONAL", "NOTE":
		return policy.SeverityInfo, nil
	case "LOW", "MINOR":
		return policy.SeverityLow, nil
	case "MEDIUM", "MODERATE", "WARNING", "WARN":
		return policy.SeverityMedium, nil
	case "HIGH", "MAJOR", "ERROR":
		return policy.SeverityHigh, nil
	case "CRITICAL", "FATAL":
		return policy.SeverityCritical, nil
	default:
		return "", fmt.Errorf("unsupported tool-native severity %q", native)
	}
}
