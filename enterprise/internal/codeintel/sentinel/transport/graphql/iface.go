package graphql

import (
	"context"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/sentinel/shared"
)

type SentinelService interface {
	GetVulnerabilitiesByIDs(ctx context.Context, ids ...int) ([]shared.Vulnerability, error)
	VulnerabilityByID(ctx context.Context, id int) (shared.Vulnerability, bool, error)
	VulnerabilityMatchByID(ctx context.Context, id int) (shared.VulnerabilityMatch, bool, error)
	GetVulnerabilities(ctx context.Context, args shared.GetVulnerabilitiesArgs) ([]shared.Vulnerability, int, error)
	GetVulnerabilityMatches(ctx context.Context, args shared.GetVulnerabilityMatchesArgs) ([]shared.VulnerabilityMatch, int, error)
}
