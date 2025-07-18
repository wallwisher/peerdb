package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"

	"github.com/PeerDB-io/peerdb/flow/generated/protos"
	"github.com/PeerDB-io/peerdb/flow/shared"
)

type NameAndExclude struct {
	Exclude map[string]struct{}
	Name    string
}

func NewNameAndExclude(name string, exclude []string) NameAndExclude {
	var exset map[string]struct{}
	if len(exclude) != 0 {
		exset = make(map[string]struct{}, len(exclude))
		for _, col := range exclude {
			exset[col] = struct{}{}
		}
	}
	return NameAndExclude{Name: name, Exclude: exset}
}

type RecordTypeCounts struct {
	InsertCount atomic.Int32
	UpdateCount atomic.Int32
	DeleteCount atomic.Int32
}

type RecordsToStreamRequest[T Items] struct {
	records                  <-chan Record[T]
	TableMapping             map[string]*RecordTypeCounts
	BatchID                  int64
	UnboundedNumericAsString bool
	TargetDWH                protos.DBType
}

func NewRecordsToStreamRequest[T Items](
	records <-chan Record[T],
	tableMapping map[string]*RecordTypeCounts,
	batchID int64,
	unboundedNumericAsString bool,
	targetDWH protos.DBType,
) *RecordsToStreamRequest[T] {
	return &RecordsToStreamRequest[T]{
		records:                  records,
		TableMapping:             tableMapping,
		BatchID:                  batchID,
		UnboundedNumericAsString: unboundedNumericAsString,
		TargetDWH:                targetDWH,
	}
}

func (r *RecordsToStreamRequest[T]) GetRecords() <-chan Record[T] {
	return r.records
}

type PullRecordsRequest[T Items] struct {
	// record batch for pushing changes into
	RecordStream *CDCStream[T]
	// ConsumedOffset can be reported as committed to reduce slot size
	ConsumedOffset *atomic.Int64
	// FlowJobName is the name of the flow job.
	FlowJobName string
	// relId to name Mapping
	SrcTableIDNameMapping map[uint32]string
	// source to destination table name mapping
	TableNameMapping map[string]NameAndExclude
	// tablename to schema mapping
	TableNameSchemaMapping map[string]*protos.TableSchema
	// overrides dynamic configuration
	Env map[string]string
	// override publication name
	OverridePublicationName string
	// override replication slot name
	OverrideReplicationSlotName string
	// LastOffset is the latest LSN that was synced.
	LastOffset CdcCheckpoint
	// MaxBatchSize is the max number of records to fetch.
	MaxBatchSize uint32
	// peerdb versioning to prevent breaking changes
	InternalVersion uint32
	// IdleTimeout is the timeout to wait for new records.
	IdleTimeout time.Duration
}

type ToJSONOptions struct {
	UnnestColumns map[string]struct{}
	HStoreAsJSON  bool
}

func NewToJSONOptions(unnestCols []string, hstoreAsJSON bool) ToJSONOptions {
	var unnestColumns map[string]struct{}
	if len(unnestCols) != 0 {
		unnestColumns = make(map[string]struct{}, len(unnestCols))
		for _, col := range unnestCols {
			unnestColumns[col] = struct{}{}
		}
	}
	return ToJSONOptions{
		UnnestColumns: unnestColumns,
		HStoreAsJSON:  hstoreAsJSON,
	}
}

type TableWithPkey struct {
	TableName string
	// SHA256 hash of the primary key columns
	PkeyColVal [32]byte
}

func RecToTablePKey[T Items](
	tableNameSchemaMapping map[string]*protos.TableSchema,
	rec Record[T],
) (TableWithPkey, error) {
	tableName := rec.GetDestinationTableName()
	hasher := sha256.New()

	for _, pkeyCol := range tableNameSchemaMapping[tableName].PrimaryKeyColumns {
		pkeyColBytes, err := rec.GetItems().GetBytesByColName(pkeyCol)
		if err != nil {
			return TableWithPkey{}, fmt.Errorf("error getting primary key column '%s' value for table '%s': %w", pkeyCol, tableName, err)
		}
		// cannot return an error
		_, _ = hasher.Write(pkeyColBytes)
	}

	return TableWithPkey{
		TableName:  tableName,
		PkeyColVal: [32]byte(hasher.Sum(nil)),
	}, nil
}

type SyncRecordsRequest[T Items] struct {
	Records *CDCStream[T]
	// ConsumedOffset allows destination to confirm lsn for slot
	ConsumedOffset *atomic.Int64
	// FlowJobName is the name of the flow job.
	FlowJobName string
	// destination table name -> schema mapping
	TableNameSchemaMapping map[string]*protos.TableSchema
	Env                    map[string]string
	// Staging path for AVRO files in CDC
	StagingPath string
	// Lua script
	Script string
	// source:destination mappings
	TableMappings []*protos.TableMapping
	SyncBatchID   int64
	Version       uint32
}

type NormalizeRecordsRequest struct {
	Env                    map[string]string
	TableNameSchemaMapping map[string]*protos.TableSchema
	FlowJobName            string
	SoftDeleteColName      string
	SyncedAtColName        string
	TableMappings          []*protos.TableMapping
	SyncBatchID            int64
	Version                uint32
}

//nolint:govet // no need to save on fieldalignment
type SyncResponse struct {
	// TableNameRowsMapping tells how many records need to be synced to each destination table.
	TableNameRowsMapping map[string]*RecordTypeCounts
	// to be carried to parent workflow
	TableSchemaDeltas []*protos.TableSchemaDelta
	// LastSyncedCheckpoint is the last state (eg LSN, GTID) that was synced.
	LastSyncedCheckpoint CdcCheckpoint
	// NumRecordsSynced is the number of records that were synced.
	NumRecordsSynced   int64
	CurrentSyncBatchID int64
	Warnings           shared.QRepWarnings
}

type NormalizeResponse struct {
	StartBatchID int64
	EndBatchID   int64
}

type RelationMessageMapping map[uint32]*pglogrepl.RelationMessage

type SyncCompositeResponse struct {
	SyncResponse   *SyncResponse
	NeedsNormalize bool
}

type SetupReplicationResult struct {
	Conn             interface{ Close(context.Context) error }
	SlotName         string
	SnapshotName     string
	SupportsTIDScans bool
}

type RemoveFlowDetailsFromCatalogRequest struct {
	FlowName string
	Resync   bool
}
