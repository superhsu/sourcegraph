package store

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/go-version"
	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/sentinel/shared"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/database/batch"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

func (s *store) VulnerabilityMatchByID(ctx context.Context, id int) (_ shared.VulnerabilityMatch, _ bool, err error) {
	ctx, _, endObservation := s.operations.vulnerabilityMatchByID.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	matches, _, err := scanVulnerabilityMatchesAndCount(s.db.Query(ctx, sqlf.Sprintf(vulnerabilityMatchByIDQuery, id)))
	if err != nil || len(matches) == 0 {
		return shared.VulnerabilityMatch{}, false, err
	}

	return matches[0], true, nil
}

const vulnerabilityMatchByIDQuery = `
SELECT
	m.id,
	m.upload_id,
	vap.vulnerability_id,
	` + vulnerabilityAffectedPackageFields + `,
	` + vulnerabilityAffectedSymbolFields + `,
	0 AS count
FROM vulnerability_matches m
LEFT JOIN vulnerability_affected_packages vap ON vap.id = m.vulnerability_affected_package_id
LEFT JOIN vulnerability_affected_symbols vas ON vas.vulnerability_affected_package_id = vap.id
WHERE m.id = %s
`

func (s *store) GetVulnerabilityMatches(ctx context.Context, args shared.GetVulnerabilityMatchesArgs) (_ []shared.VulnerabilityMatch, _ int, err error) {
	ctx, _, endObservation := s.operations.getVulnerabilityMatches.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	return scanVulnerabilityMatchesAndCount(s.db.Query(ctx, sqlf.Sprintf(getVulnerabilityMatchesQuery, args.Limit, args.Offset)))
}

const getVulnerabilityMatchesQuery = `
WITH limited_matches AS (
	SELECT
		m.id,
		m.upload_id,
		m.vulnerability_affected_package_id,
		COUNT(*) OVER() AS count
	FROM vulnerability_matches m
	ORDER BY id
	LIMIT %s OFFSET %s
)
SELECT
	m.id,
	m.upload_id,
	vap.vulnerability_id,
	` + vulnerabilityAffectedPackageFields + `,
	` + vulnerabilityAffectedSymbolFields + `,
	m.count
FROM limited_matches m
LEFT JOIN vulnerability_affected_packages vap ON vap.id = m.vulnerability_affected_package_id
LEFT JOIN vulnerability_affected_symbols vas ON vas.vulnerability_affected_package_id = vap.id
ORDER BY m.id, vap.id, vas.id
`

var flattenMatches = func(ms []shared.VulnerabilityMatch) []shared.VulnerabilityMatch {
	flattened := []shared.VulnerabilityMatch{}
	for _, m := range ms {
		i := len(flattened) - 1
		if len(flattened) == 0 || flattened[i].ID != m.ID {
			flattened = append(flattened, m)
		} else {
			if flattened[i].AffectedPackage.PackageName == "" {
				flattened[i].AffectedPackage = m.AffectedPackage
			} else {
				symbols := flattened[i].AffectedPackage.AffectedSymbols
				symbols = append(symbols, m.AffectedPackage.AffectedSymbols...)
				flattened[i].AffectedPackage.AffectedSymbols = symbols
			}
		}
	}

	return flattened
}

var scanVulnerabilityMatchesAndCount = func(rows basestore.Rows, queryErr error) ([]shared.VulnerabilityMatch, int, error) {
	matches, totalCount, err := basestore.NewSliceWithCountScanner(func(s dbutil.Scanner) (match shared.VulnerabilityMatch, count int, _ error) {
		var (
			vap     shared.AffectedPackage
			vas     shared.AffectedSymbol
			fixedIn string
		)

		if err := s.Scan(
			&match.ID,
			&match.UploadID,
			&match.VulnerabilityID,
			// RHS(s) of left join (may be null)
			&dbutil.NullString{S: &vap.PackageName},
			&dbutil.NullString{S: &vap.Language},
			&dbutil.NullString{S: &vap.Namespace},
			pq.Array(&vap.VersionConstraint),
			&dbutil.NullBool{B: &vap.Fixed},
			&dbutil.NullString{S: &fixedIn},
			&dbutil.NullString{S: &vas.Path},
			pq.Array(vas.Symbols),
			&count,
		); err != nil {
			return shared.VulnerabilityMatch{}, 0, err
		}

		if fixedIn != "" {
			vap.FixedIn = &fixedIn
		}
		if vas.Path != "" {
			vap.AffectedSymbols = append(vap.AffectedSymbols, vas)
		}
		if vap.PackageName != "" {
			match.AffectedPackage = vap
		}

		return match, count, nil
	})(rows, queryErr)
	if err != nil {
		return nil, 0, err
	}

	return flattenMatches(matches), totalCount, nil
}

func (s *store) ScanMatches(ctx context.Context, batchSize int) (numReferencesScanned int, numVulnerabilityMatches int, err error) {
	ctx, _, endObservation := s.operations.scanMatches.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	tx, err := s.db.Transact(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { err = tx.Done(err) }()

	numScanned := 0
	scanFilteredVulnerabilityMatches := basestore.NewFilteredSliceScanner(func(s dbutil.Scanner) (m VulnerabilityMatch, _ bool, _ error) {
		var (
			version            string
			versionConstraints []string
		)

		if err := s.Scan(&m.UploadID, &m.VulnerabilityAffectedPackageID, &version, pq.Array(&versionConstraints)); err != nil {
			return VulnerabilityMatch{}, false, err
		}

		numScanned++
		matches, valid := versionMatchesConstraints(version, versionConstraints)
		_ = valid // TODO - log un-parseable versions

		return m, matches, nil
	})

	matches, err := scanFilteredVulnerabilityMatches(tx.Query(ctx, sqlf.Sprintf(
		scanMatchesQuery,
		batchSize,
		sqlf.Join(makeSchemeTtoVulnerabilityLanguageMappingConditions(), " OR "),
	)))
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Exec(ctx, sqlf.Sprintf(scanMatchesTemporaryTableQuery)); err != nil {
		return 0, 0, err
	}

	if err := batch.WithInserter(
		ctx,
		tx.Handle(),
		"t_vulnerability_affected_packages",
		batch.MaxNumPostgresParameters,
		[]string{
			"upload_id",
			"vulnerability_affected_package_id",
		},
		func(inserter *batch.Inserter) error {
			for _, match := range matches {
				if err := inserter.Insert(
					ctx,
					match.UploadID,
					match.VulnerabilityAffectedPackageID,
				); err != nil {
					return err
				}
			}

			return nil
		},
	); err != nil {
		return 0, 0, err
	}

	numMatched, _, err := basestore.ScanFirstInt(tx.Query(ctx, sqlf.Sprintf(scanMatchesUpdateQuery)))
	if err != nil {
		return 0, 0, err
	}

	return numScanned, numMatched, nil
}

var scipSchemeToVulnerabilityLanguage = map[string]string{
	"gomod": "go",
	"npm":   "Javascript",
	// TODO - java mapping
}

func makeSchemeTtoVulnerabilityLanguageMappingConditions() []*sqlf.Query {
	schemes := make([]string, 0, len(scipSchemeToVulnerabilityLanguage))
	for scheme := range scipSchemeToVulnerabilityLanguage {
		schemes = append(schemes, scheme)
	}
	sort.Strings(schemes)

	mappings := make([]*sqlf.Query, 0, len(schemes))
	for _, scheme := range schemes {
		mappings = append(mappings, sqlf.Sprintf("(r.scheme = %s AND vap.language = %s)", scheme, scipSchemeToVulnerabilityLanguage[scheme]))
	}

	return mappings
}

const scanMatchesQuery = `
WITH
candidates AS (
	SELECT u.id
	FROM lsif_uploads u
	JOIN repo r ON r.id = u.repository_id
	WHERE
		u.state = 'completed' AND
		r.deleted_at IS NULL AND
		r.blocked IS NULL AND
		NOT EXISTS (
			SELECT 1
			FROM lsif_uploads_vulnerability_scan uvs
			WHERE
				uvs.upload_id = u.id AND
				-- TODO: we'd rather compare this against vuln update times
				uvs.last_scanned_at < NOW()
		)
	ORDER BY u.id
	LIMIT %s
),
locked_candidates AS (
	INSERT INTO lsif_uploads_vulnerability_scan (upload_id, last_scanned_at)
	SELECT id, NOW() FROM candidates
	ON CONFLICT DO NOTHING
	RETURNING upload_id
)
SELECT
	r.dump_id,
	vap.id,
	r.version,
	vap.version_constraint
FROM locked_candidates lc
JOIN lsif_references r ON r.dump_id = lc.upload_id
JOIN vulnerability_affected_packages vap ON
	-- NOTE: This is currently a bit of a hack that works to find some
	-- good matches with the dataset we have. We should have a better
	-- way to match on a normalized name here, or have rules per types
	-- of language ecosystem.
	r.name LIKE '%%' || vap.package_name || '%%'
WHERE %s
`

const scanMatchesTemporaryTableQuery = `
CREATE TEMPORARY TABLE t_vulnerability_affected_packages (
	upload_id                          INT NOT NULL,
	vulnerability_affected_package_id  INT NOT NULL
) ON COMMIT DROP
`

const scanMatchesUpdateQuery = `
WITH ins AS (
	INSERT INTO vulnerability_matches (upload_id, vulnerability_affected_package_id)
	SELECT upload_id, vulnerability_affected_package_id FROM t_vulnerability_affected_packages
	ON CONFLICT DO NOTHING
	RETURNING 1
)
SELECT COUNT(*) FROM ins
`

type VulnerabilityMatch struct {
	UploadID                       int
	VulnerabilityAffectedPackageID int
}

func versionMatchesConstraints(versionString string, constraints []string) (matches, valid bool) {
	v, err := version.NewVersion(versionString)
	if err != nil {
		return false, false
	}

	constraint, err := version.NewConstraint(strings.Join(constraints, ","))
	if err != nil {
		return false, false
	}

	return constraint.Check(v), true
}
