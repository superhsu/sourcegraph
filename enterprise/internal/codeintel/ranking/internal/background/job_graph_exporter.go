package background

import (
	"context"
	"time"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/ranking/internal/lsifstore"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/ranking/internal/store"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/shared/background"
	"github.com/sourcegraph/sourcegraph/internal/goroutine"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

const recordTypeName = "path count inputs"

func NewSymbolExporter(
	observationCtx *observation.Context,
	store store.Store,
	lsifstore lsifstore.LsifStore,
	interval time.Duration,
	readBatchSize int,
	writeBatchSize int,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.symbol-exporter"

	return background.NewPipelineJob(context.Background(), background.PipelineOptions{
		Name:        name,
		Description: "Exports SCIP data to ranking definitions and reference tables.",
		Interval:    interval,
		Metrics:     background.NewPipelineMetrics(observationCtx, name, recordTypeName),
		ProcessFunc: func(ctx context.Context) (numRecordsProcessed int, numRecordsAltered background.TaggedCounts, err error) {
			numUploadsScanned, numDefinitionsInserted, numReferencesInserted, err := exportRankingGraph(
				ctx,
				store,
				lsifstore,
				observationCtx.Logger,
				readBatchSize,
				writeBatchSize,
			)

			m := map[string]int{
				"definitions": numDefinitionsInserted,
				"references":  numReferencesInserted,
			}
			return numUploadsScanned, background.NewMapCount(m), err
		},
	})
}

func NewSymbolDefinitionsJanitor(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.symbol-definitions-janitor"

	return background.NewJanitorJob(context.Background(), background.JanitorOptions{
		Name:        name,
		Description: "Removes stale data from the ranking definitions table.",
		Interval:    interval,
		Metrics:     background.NewJanitorMetrics(observationCtx, name, recordTypeName),
		CleanupFunc: func(ctx context.Context) (numRecordsScanned int, numRecordsAltered int, err error) {
			return vacuumStaleDefinitions(ctx, store)
		},
	})
}

func NewSymbolReferencesJanitor(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.symbol-references-janitor"

	return background.NewJanitorJob(context.Background(), background.JanitorOptions{
		Name:        name,
		Description: "Removes stale data from the ranking references table.",
		Interval:    interval,
		Metrics:     background.NewJanitorMetrics(observationCtx, name, recordTypeName),
		CleanupFunc: func(ctx context.Context) (numRecordsScanned int, numRecordsAltered int, err error) {
			return vacuumStaleReferences(ctx, store)
		},
	})
}

func NewSymbolInitialPathsJanitor(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.symbol-initial-paths-janitor"

	return background.NewJanitorJob(context.Background(), background.JanitorOptions{
		Name:        name,
		Description: "Removes stale data from the ranking initial paths table.",
		Interval:    interval,
		Metrics:     background.NewJanitorMetrics(observationCtx, name, recordTypeName),
		CleanupFunc: func(ctx context.Context) (numRecordsScanned int, numRecordsAltered int, err error) {
			return vacuumStaleInitialPaths(ctx, store)
		},
	})
}

func NewRankCountsJanitor(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.rank-counts-janitor"

	return background.NewJanitorJob(context.Background(), background.JanitorOptions{
		Name:        name,
		Description: "Removes old path count input records.",
		Interval:    interval,
		Metrics:     background.NewJanitorMetrics(observationCtx, name, recordTypeName),
		CleanupFunc: func(ctx context.Context) (numRecordsScanned int, numRecordsAltered int, err error) {
			return vacuumStaleGraphs(ctx, store)
		},
	})
}

func NewRankJanitor(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.rank-janitor"

	return background.NewJanitorJob(context.Background(), background.JanitorOptions{
		Name:        name,
		Description: "Removes stale ranking data.",
		Interval:    interval,
		Metrics:     background.NewJanitorMetrics(observationCtx, name, recordTypeName),
		CleanupFunc: func(ctx context.Context) (numRecordsScanned int, numRecordsAltered int, err error) {
			return vacuumStaleRanks(ctx, store)
		},
	})
}

func NewMapper(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
	batchSize int,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.file-reference-count-mapper"

	return background.NewPipelineJob(context.Background(), background.PipelineOptions{
		Name:        name,
		Description: "Joins ranking definition and references together to create document path count records.",
		Interval:    interval,
		Metrics:     background.NewPipelineMetrics(observationCtx, name, recordTypeName),
		ProcessFunc: func(ctx context.Context) (numRecordsProcessed int, numRecordsAltered background.TaggedCounts, err error) {
			numReferencesScanned, nuPathCountInputsInserted, err := mapRankingGraph(ctx, store, batchSize)
			if err != nil {
				return 0, nil, err
			}

			return numReferencesScanned, background.NewSingleCount(nuPathCountInputsInserted), err
		},
	})
}

func NewSeedMapper(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
	batchSize int,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.file-reference-count-seed-mapper"

	return background.NewPipelineJob(context.Background(), background.PipelineOptions{
		Name:        name,
		Description: "Adds initial zero counts to files that may not contain any known references.",
		Interval:    interval,
		Metrics:     background.NewPipelineMetrics(observationCtx, name, recordTypeName),
		ProcessFunc: func(ctx context.Context) (numRecordsProcessed int, numRecordsAltered background.TaggedCounts, err error) {
			numInitialPathsScanned, nuPathCountInputsInserted, err := mapInitializerRankingGraph(ctx, store, batchSize)
			if err != nil {
				return 0, nil, err
			}

			return numInitialPathsScanned, background.NewSingleCount(nuPathCountInputsInserted), err
		},
	})
}

func NewReducer(
	observationCtx *observation.Context,
	store store.Store,
	interval time.Duration,
	batchSize int,
) goroutine.BackgroundRoutine {
	name := "codeintel.ranking.file-reference-count-reducer"

	return background.NewPipelineJob(context.Background(), background.PipelineOptions{
		Name:        name,
		Description: "Aggregates records from `codeintel_ranking_path_counts_inputs` into `codeintel_path_ranks`.",
		Interval:    interval,
		Metrics:     background.NewPipelineMetrics(observationCtx, name, recordTypeName),
		ProcessFunc: func(ctx context.Context) (numRecordsProcessed int, numRecordsAltered background.TaggedCounts, err error) {
			numPathCountInputsScanned, numRanksUpdated, err := reduceRankingGraph(ctx, store, batchSize)
			return numPathCountInputsScanned, background.NewSingleCount(numRanksUpdated), err
		},
	})
}
