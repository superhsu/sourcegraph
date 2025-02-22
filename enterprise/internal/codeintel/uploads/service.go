package uploads

import (
	"context"
	"time"

	"cloud.google.com/go/storage"
	"github.com/derision-test/glock"
	"github.com/opentracing/opentracing-go/log"
	logger "github.com/sourcegraph/log"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/shared/types"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/uploads/internal/lsifstore"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/uploads/internal/store"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/uploads/shared"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	dbworkerstore "github.com/sourcegraph/sourcegraph/internal/workerutil/dbworker/store"
	"github.com/sourcegraph/sourcegraph/lib/codeintel/precise"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

type Service struct {
	store           store.Store
	repoStore       RepoStore
	workerutilStore dbworkerstore.Store[types.Upload]
	lsifstore       lsifstore.LsifStore
	gitserverClient gitserver.Client
	rankingBucket   *storage.BucketHandle
	policySvc       PolicyService
	policyMatcher   PolicyMatcher
	locker          Locker
	logger          logger.Logger
	operations      *operations
	clock           glock.Clock
}

func newService(
	observationCtx *observation.Context,
	store store.Store,
	repoStore RepoStore,
	lsifstore lsifstore.LsifStore,
	gsc gitserver.Client,
	rankingBucket *storage.BucketHandle,
	policySvc PolicyService,
	policyMatcher PolicyMatcher,
	locker Locker,
) *Service {
	workerutilStore := store.WorkerutilStore(observationCtx)

	return &Service{
		store:           store,
		repoStore:       repoStore,
		workerutilStore: workerutilStore,
		lsifstore:       lsifstore,
		gitserverClient: gsc,
		rankingBucket:   rankingBucket,
		policySvc:       policySvc,
		policyMatcher:   policyMatcher,
		locker:          locker,
		logger:          observationCtx.Logger,
		operations:      newOperations(observationCtx),
		clock:           glock.NewRealClock(),
	}
}

func (s *Service) GetCommitsVisibleToUpload(ctx context.Context, uploadID, limit int, token *string) ([]string, *string, error) {
	return s.store.GetCommitsVisibleToUpload(ctx, uploadID, limit, token)
}

func (s *Service) GetCommitGraphMetadata(ctx context.Context, repositoryID int) (bool, *time.Time, error) {
	return s.store.GetCommitGraphMetadata(ctx, repositoryID)
}

// TODO(#48681) - Move to autoindexing/internal/background
func (s *Service) GetRepositoriesForIndexScan(ctx context.Context, table, column string, processDelay time.Duration, allowGlobalPolicies bool, repositoryMatchLimit *int, limit int, now time.Time) ([]int, error) {
	return s.store.GetRepositoriesForIndexScan(ctx, table, column, processDelay, allowGlobalPolicies, repositoryMatchLimit, limit, now)
}

func (s *Service) GetDirtyRepositories(ctx context.Context) (_ []shared.DirtyRepository, err error) {
	return s.store.GetDirtyRepositories(ctx)
}

// TODO(#48681) - Used by autoindexing/transport/graphql
func (s *Service) GetIndexers(ctx context.Context, opts shared.GetIndexersOptions) ([]string, error) {
	return s.store.GetIndexers(ctx, opts)
}

func (s *Service) GetUploads(ctx context.Context, opts shared.GetUploadsOptions) ([]types.Upload, int, error) {
	return s.store.GetUploads(ctx, opts)
}

func (s *Service) GetUploadByID(ctx context.Context, id int) (types.Upload, bool, error) {
	return s.store.GetUploadByID(ctx, id)
}

func (s *Service) GetUploadsByIDs(ctx context.Context, ids ...int) ([]types.Upload, error) {
	return s.store.GetUploadsByIDs(ctx, ids...)
}

func (s *Service) GetUploadIDsWithReferences(ctx context.Context, orderedMonikers []precise.QualifiedMonikerData, ignoreIDs []int, repositoryID int, commit string, limit int, offset int) ([]int, int, int, error) {
	return s.store.GetUploadIDsWithReferences(ctx, orderedMonikers, ignoreIDs, repositoryID, commit, limit, offset, nil)
}

func (s *Service) DeleteUploadByID(ctx context.Context, id int) (bool, error) {
	return s.store.DeleteUploadByID(ctx, id)
}

func (s *Service) DeleteUploads(ctx context.Context, opts shared.DeleteUploadsOptions) error {
	return s.store.DeleteUploads(ctx, opts)
}

func (s *Service) GetRepositoriesMaxStaleAge(ctx context.Context) (_ time.Duration, err error) {
	return s.store.GetRepositoriesMaxStaleAge(ctx)
}

// numAncestors is the number of ancestors to query from gitserver when trying to find the closest
// ancestor we have data for. Setting this value too low (relative to a repository's commit rate)
// will cause requests for an unknown commit return too few results; setting this value too high
// will raise the latency of requests for an unknown commit.
//
// TODO(efritz) - make adjustable via site configuration
const numAncestors = 100

// inferClosestUploads will return the set of visible uploads for the given commit. If this commit is
// newer than our last refresh of the lsif_nearest_uploads table for this repository, then we will mark
// the repository as dirty and quickly approximate the correct set of visible uploads.
//
// Because updating the entire commit graph is a blocking, expensive, and lock-guarded process, we  want
// to only do that in the background and do something chearp in latency-sensitive paths. To construct an
// approximate result, we query gitserver for a (relatively small) set of ancestors for the given commit,
// correlate that with the upload data we have for those commits, and re-run the visibility algorithm over
// the graph. This will not always produce the full set of visible commits - some responses may not contain
// all results while a subsequent request made after the lsif_nearest_uploads has been updated to include
// this commit will.
func (s *Service) InferClosestUploads(ctx context.Context, repositoryID int, commit, path string, exactPath bool, indexer string) (_ []types.Dump, err error) {
	ctx, _, endObservation := s.operations.inferClosestUploads.With(ctx, &err, observation.Args{
		LogFields: []log.Field{log.Int("repositoryID", repositoryID), log.String("commit", commit), log.String("path", path), log.Bool("exactPath", exactPath), log.String("indexer", indexer)},
	})
	defer endObservation(1, observation.Args{})

	repo, err := s.repoStore.Get(ctx, api.RepoID(repositoryID))
	if err != nil {
		return nil, err
	}

	// The parameters exactPath and rootMustEnclosePath align here: if we're looking for dumps
	// that can answer queries for a directory (e.g. diagnostics), we want any dump that happens
	// to intersect the target directory. If we're looking for dumps that can answer queries for
	// a single file, then we need a dump with a root that properly encloses that file.
	if dumps, err := s.store.FindClosestDumps(ctx, repositoryID, commit, path, exactPath, indexer); err != nil {
		return nil, errors.Wrap(err, "store.FindClosestDumps")
	} else if len(dumps) != 0 {
		return dumps, nil
	}

	// Repository has no LSIF data at all
	if repositoryExists, err := s.store.HasRepository(ctx, repositoryID); err != nil {
		return nil, errors.Wrap(err, "dbstore.HasRepository")
	} else if !repositoryExists {
		return nil, nil
	}

	// Commit is known and the empty dumps list explicitly means nothing is visible
	if commitExists, err := s.store.HasCommit(ctx, repositoryID, commit); err != nil {
		return nil, errors.Wrap(err, "dbstore.HasCommit")
	} else if commitExists {
		return nil, nil
	}

	// Otherwise, the repository has LSIF data but we don't know about the commit. This commit
	// is probably newer than our last upload. Pull back a portion of the updated commit graph
	// and try to link it with what we have in the database. Then mark the repository's commit
	// graph as dirty so it's updated for subsequent requests.

	graph, err := s.gitserverClient.CommitGraph(ctx, repo.Name, gitserver.CommitGraphOptions{
		Commit: commit,
		Limit:  numAncestors,
	})
	if err != nil {
		return nil, errors.Wrap(err, "gitserverClient.CommitGraph")
	}

	dumps, err := s.store.FindClosestDumpsFromGraphFragment(ctx, repositoryID, commit, path, exactPath, indexer, graph)
	if err != nil {
		return nil, errors.Wrap(err, "dbstore.FindClosestDumpsFromGraphFragment")
	}

	if err := s.store.SetRepositoryAsDirty(ctx, repositoryID); err != nil {
		return nil, errors.Wrap(err, "dbstore.MarkRepositoryAsDirty")
	}

	return dumps, nil
}

func (s *Service) GetDumpsWithDefinitionsForMonikers(ctx context.Context, monikers []precise.QualifiedMonikerData) ([]types.Dump, error) {
	return s.store.GetDumpsWithDefinitionsForMonikers(ctx, monikers)
}

func (s *Service) GetDumpsByIDs(ctx context.Context, ids []int) ([]types.Dump, error) {
	return s.store.GetDumpsByIDs(ctx, ids)
}

func (s *Service) ReferencesForUpload(ctx context.Context, uploadID int) (shared.PackageReferenceScanner, error) {
	return s.store.ReferencesForUpload(ctx, uploadID)
}

func (s *Service) GetAuditLogsForUpload(ctx context.Context, uploadID int) ([]types.UploadLog, error) {
	return s.store.GetAuditLogsForUpload(ctx, uploadID)
}

func (s *Service) GetUploadDocumentsForPath(ctx context.Context, bundleID int, pathPattern string) ([]string, int, error) {
	return s.lsifstore.GetUploadDocumentsForPath(ctx, bundleID, pathPattern)
}

func (s *Service) GetRecentUploadsSummary(ctx context.Context, repositoryID int) ([]shared.UploadsWithRepositoryNamespace, error) {
	return s.store.GetRecentUploadsSummary(ctx, repositoryID)
}

func (s *Service) GetLastUploadRetentionScanForRepository(ctx context.Context, repositoryID int) (*time.Time, error) {
	return s.store.GetLastUploadRetentionScanForRepository(ctx, repositoryID)
}

func (s *Service) ReindexUploads(ctx context.Context, opts shared.ReindexUploadsOptions) error {
	return s.store.ReindexUploads(ctx, opts)
}

func (s *Service) ReindexUploadByID(ctx context.Context, id int) error {
	return s.store.ReindexUploadByID(ctx, id)
}
