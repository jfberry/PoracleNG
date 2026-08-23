package api

import (
	"context"
	"crypto/md5" //nolint:gosec // not security-sensitive; matches legacy geofence-hash digest
	"encoding/json"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// geofenceAllOutput is the typed body for GET /api/geofence/all: a status
// envelope plus the full list of loaded geofences.
type geofenceAllOutput struct {
	Body struct {
		Status   string           `json:"status"`
		Geofence []geofence.Fence `json:"geofence"`
	}
}

// RegisterGeofenceAll registers GET /api/geofence/all, returning all geofence
// data. Replaces the legacy gin HandleGeofenceAll. Body is {status, geofence}.
func RegisterGeofenceAll(api huma.API, stateMgr *state.Manager) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-all", Method: "GET", Path: "/geofence/all",
		Summary: "All geofence data", Tags: []string{"geofence"},
		Description: "Returns every loaded geofence (file- and Koji-sourced) as {status, geofence}: name, display metadata, and polygon path per fence.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*geofenceAllOutput, error) {
		st := stateMgr.Get()
		out := &geofenceAllOutput{}
		out.Body.Status = "ok"
		out.Body.Geofence = st.Fences
		return out, nil
	})
}

// geofenceHashOutput is the typed body for GET /api/geofence/all/hash: a status
// envelope plus a per-area-name MD5 hash of the area's path.
type geofenceHashOutput struct {
	Body struct {
		Status string            `json:"status"`
		Areas  map[string]string `json:"areas"`
	}
}

// RegisterGeofenceHash registers GET /api/geofence/all/hash, returning MD5
// hashes of each geofence path. Replaces the legacy gin HandleGeofenceHash.
// Body is {status, areas}.
func RegisterGeofenceHash(api huma.API, stateMgr *state.Manager) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-hash", Method: "GET", Path: "/geofence/all/hash",
		Summary: "MD5 hashes of geofence paths", Tags: []string{"geofence"},
		Description: "Returns {status, areas}: an MD5 hash of each geofence's polygon path keyed by area name, so clients can detect geofence changes without downloading the full polygon data.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*geofenceHashOutput, error) {
		st := stateMgr.Get()
		areas := make(map[string]string, len(st.Fences))
		for _, f := range st.Fences {
			pathJSON, _ := json.Marshal(f.Path)
			areas[f.Name] = fmt.Sprintf("%x", md5.Sum(pathJSON)) //nolint:gosec // see import note
		}
		out := &geofenceHashOutput{}
		out.Body.Status = "ok"
		out.Body.Areas = areas
		return out, nil
	})
}

// geoJSONProperties mirrors the per-feature properties block emitted for each
// geofence. Field names/casing match the legacy map keys exactly.
type geoJSONProperties struct {
	Name             string `json:"name"`
	Color            string `json:"color"`
	ID               int    `json:"id"`
	Group            string `json:"group"`
	Description      string `json:"description"`
	UserSelectable   bool   `json:"userSelectable"`
	DisplayInMatches bool   `json:"displayInMatches"`
}

// geoJSONGeometry is the geometry block for a feature. coordinates is left open
// (any) because the nesting depth differs for Polygon vs MultiPolygon; the
// marshaled value is byte-identical to the legacy handler.
type geoJSONGeometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

// geoJSONFeature is a single GeoJSON Feature.
type geoJSONFeature struct {
	Type       string            `json:"type"`
	Properties geoJSONProperties `json:"properties"`
	Geometry   geoJSONGeometry   `json:"geometry"`
}

// geofenceGeoJSONOutput is the typed body for GET /api/geofence/all/geojson.
type geofenceGeoJSONOutput struct {
	Body struct {
		Status  string `json:"status"`
		GeoJSON struct {
			Type     string           `json:"type"`
			Features []geoJSONFeature `json:"features"`
		} `json:"geoJSON"`
	}
}

// RegisterGeofenceGeoJSON registers GET /api/geofence/all/geojson, returning
// geofences as a GeoJSON FeatureCollection. Replaces the legacy gin
// HandleGeofenceGeoJSON. Body is {status, geoJSON}.
func RegisterGeofenceGeoJSON(api huma.API, stateMgr *state.Manager) {
	huma.Register(api, huma.Operation{
		OperationID: "get-geofence-geojson", Method: "GET", Path: "/geofence/all/geojson",
		Summary: "Geofences as a GeoJSON FeatureCollection", Tags: []string{"geofence"},
		Description: "Exports all loaded geofences as a standard GeoJSON FeatureCollection wrapped in {status, geoJSON}, suitable for map display or import into other tools.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*geofenceGeoJSONOutput, error) {
		st := stateMgr.Get()

		features := make([]geoJSONFeature, 0, len(st.Fences))
		for _, f := range st.Fences {
			properties := geoJSONProperties{
				Name:             f.Name,
				Color:            f.Color,
				ID:               f.ID,
				Group:            f.Group,
				Description:      f.Description,
				UserSelectable:   f.UserSelectable,
				DisplayInMatches: f.DisplayInMatches,
			}

			var geomType string
			var coordinates any

			if len(f.Multipath) > 0 {
				geomType = "MultiPolygon"
				multiCoords := make([][][][2]float64, len(f.Multipath))
				for i, subpath := range f.Multipath {
					ring := make([][2]float64, len(subpath))
					for j, coord := range subpath {
						ring[j] = [2]float64{coord[1], coord[0]} // GeoJSON is [lon, lat]
					}
					if len(ring) > 0 && ring[len(ring)-1] != ring[0] {
						ring = append(ring, ring[0])
					}
					multiCoords[i] = [][][2]float64{ring}
				}
				coordinates = multiCoords
			} else {
				geomType = "Polygon"
				ring := make([][2]float64, len(f.Path))
				for i, coord := range f.Path {
					ring[i] = [2]float64{coord[1], coord[0]} // GeoJSON is [lon, lat]
				}
				if len(ring) > 0 && ring[len(ring)-1] != ring[0] {
					ring = append(ring, ring[0])
				}
				coordinates = [][][2]float64{ring}
			}

			features = append(features, geoJSONFeature{
				Type:       "Feature",
				Properties: properties,
				Geometry: geoJSONGeometry{
					Type:        geomType,
					Coordinates: coordinates,
				},
			})
		}

		out := &geofenceGeoJSONOutput{}
		out.Body.Status = "ok"
		out.Body.GeoJSON.Type = "FeatureCollection"
		out.Body.GeoJSON.Features = features
		return out, nil
	})
}

// configSchemaOutput is the typed body for GET /api/config/schema: a status
// envelope plus the config editor section list.
type configSchemaOutput struct {
	Body struct {
		Status   string          `json:"status"`
		Sections []ConfigSection `json:"sections"`
	}
}

// RegisterConfigSchema registers GET /api/config/schema, returning the config
// schema for the editor UI. Replaces the legacy gin HandleConfigSchema. Body is
// {status, sections}.
func RegisterConfigSchema(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-config-schema", Method: "GET", Path: "/config/schema",
		Summary: "Config editor schema", Tags: []string{"config"},
		Description: "Returns {status, sections}: per-section field metadata (names, types, defaults, descriptions) describing the editable config surface. Drives the config-editor UI; pair with GET /config/values for current values.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*configSchemaOutput, error) {
		out := &configSchemaOutput{}
		out.Body.Status = "ok"
		out.Body.Sections = configSchema
		return out, nil
	})
}

// masterdataMonstersInput carries the optional locale query param.
type masterdataMonstersInput struct {
	Locale string `query:"locale"`
}

// RegisterMasterdataMonsters registers GET /api/masterdata/monsters, building
// the poracle-v2 monsters map from raw masterfile data + translations. Replaces
// the legacy gin HandleMasterdataMonsters. The map is re-marshalled by huma to
// the same JSON the gin handler produced via c.JSON.
func RegisterMasterdataMonsters(api huma.API, gd *gamedata.GameData, translations *i18n.Bundle) {
	huma.Register(api, huma.Operation{
		OperationID: "get-masterdata-monsters", Method: "GET", Path: "/masterdata/monsters",
		Summary:     "All pokemon with names, forms, types",
		Description: "Returns the poracle-v2 monsters map keyed by `<id>_<form>`. The response body is left open (freeform): it is an object with arbitrary id-keyed entries (and an empty `[]` when game data is unavailable), so it has no fixed schema.",
		Tags:        []string{"masterdata"},
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, in *masterdataMonstersInput) (*anyBodyOutput, error) {
		if gd == nil {
			return &anyBodyOutput{Body: []any{}}, nil
		}
		locale := in.Locale
		if locale == "" {
			locale = "en"
		}
		tr := translations.For(locale)

		nameMap := make(map[int]string)
		for key := range gd.Monsters {
			if _, ok := nameMap[key.ID]; !ok {
				nameMap[key.ID] = tr.T(fmt.Sprintf("poke_%d", key.ID))
			}
		}

		result := make(map[string]*poracle2Monster, len(gd.Monsters))
		for key, mon := range gd.Monsters {
			types := make([]poracle2TypeEntry, len(mon.Types))
			for i, tid := range mon.Types {
				types[i] = poracle2TypeEntry{
					ID:   tid,
					Name: tr.T(fmt.Sprintf("poke_type_%d", tid)),
				}
			}

			formName := ""
			if key.Form != 0 {
				formName = tr.T(fmt.Sprintf("form_%d", key.Form))
				if formName == fmt.Sprintf("form_%d", key.Form) {
					formName = ""
				}
			}

			evolutions := make([]poracle2Evo, len(mon.Evolutions))
			for i, evo := range mon.Evolutions {
				evolutions[i] = poracle2Evo{
					EvoID:     evo.PokemonID,
					ID:        evo.FormID,
					CandyCost: evo.CandyCost,
				}
			}

			mapKey := fmt.Sprintf("%d_%d", key.ID, key.Form)
			result[mapKey] = &poracle2Monster{
				Name:  nameMap[key.ID],
				ID:    key.ID,
				Types: types,
				Form: poracle2FormEntry{
					Name: formName,
					ID:   key.Form,
				},
				Stats: poracle2Stats{
					BaseAttack:  mon.Attack,
					BaseDefense: mon.Defense,
					BaseStamina: mon.Stamina,
				},
				Evolutions: evolutions,
			}
		}

		return &anyBodyOutput{Body: result}, nil
	})
}

// masterdataGruntsOutput is the typed body for the grunts read: an object keyed
// by grunt id (string) → *poracle2Grunt. The dynamic keys make huma emit an
// object-with-additionalProperties schema that documents the VALUE shape
// (poracle2Grunt) while leaving keys arbitrary. buildGruntsResponse always
// returns a (possibly empty) map, so the wire JSON is byte-identical to the
// legacy c.Data(json.Marshal(map)) output.
type masterdataGruntsOutput struct {
	Body map[string]*poracle2Grunt
}

// RegisterMasterdataGrunts registers GET /api/masterdata/grunts, building the
// poracle-v2 grunts map from classic.json grunt data. Replaces the legacy gin
// HandleMasterdataGrunts. The map is re-marshalled by huma to the same JSON the
// gin handler produced via c.Data (json.Marshal of the same value).
func RegisterMasterdataGrunts(api huma.API, gd *gamedata.GameData) {
	// Build the response once since game data is loaded at startup.
	result := buildGruntsResponse(gd)

	huma.Register(api, huma.Operation{
		OperationID: "get-masterdata-grunts", Method: "GET", Path: "/masterdata/grunts",
		Summary: "Grunt types",
		Description: "Returns the poracle-v2 grunts map keyed by grunt id (an empty object when game data is unavailable). Keys are " +
			"arbitrary grunt ids; each value is a poracle2Grunt (type, gender, grunt category, per-slot reward flags, encounters).",
		Tags:     []string{"masterdata"},
		Security: []map[string][]string{{"poracleSecret": {}}},
	}, func(_ context.Context, _ *struct{}) (*masterdataGruntsOutput, error) {
		return &masterdataGruntsOutput{Body: result}, nil
	})
}

// SnapshotReader reads a stored snapshot by key. *snapshots.pogrebStore (via
// the snapshots.Store interface) satisfies this; a minimal interface keeps the
// Register signature testable.
type SnapshotReader interface {
	Read(ctx context.Context, key string) (*snapshots.Snapshot, error)
}

// snapshotGetInput carries the messageID path param and required target query.
type snapshotGetInput struct {
	MessageID string `path:"messageID"`
	Target    string `query:"target" required:"true"`
}

// snapshotGetOutput is the typed body for GET /api/snapshots/{messageID}: the
// stored snapshot record itself (no envelope, matching the legacy handler).
type snapshotGetOutput struct {
	Body *snapshots.Snapshot
}

// RegisterSnapshotGet registers GET /api/snapshots/{messageID}, returning the
// stored Snapshot for a delivered message. Replaces the legacy gin
// HandleSnapshotGet. A nil store (snapshots disabled) yields 503; a missing
// snapshot yields 404; a closing store yields 503; other errors yield 500.
func RegisterSnapshotGet(api huma.API, store SnapshotReader) {
	huma.Register(api, huma.Operation{
		OperationID: "get-snapshot", Method: "GET", Path: "/snapshots/{messageID}",
		Summary: "Inspect a delivered-message snapshot", Tags: []string{"snapshots"},
		Description: "Returns the stored enrichment snapshot for a delivered message, addressed by message id plus the `target` query (destination id). Admin diagnostics for the buttons/snapshots feature. 503 when [snapshots] is disabled; 404 when the snapshot has expired or never existed.",
		Security:    []map[string][]string{{"poracleSecret": {}}},
	}, func(ctx context.Context, in *snapshotGetInput) (*snapshotGetOutput, error) {
		// proc.snapshotStore is a nil snapshots.Store interface when
		// [snapshots] enabled = false; passed through the SnapshotReader
		// param it stays a nil interface, so this check surfaces 503,
		// matching the legacy concrete nil check.
		if store == nil {
			return nil, huma.Error503ServiceUnavailable("snapshots disabled")
		}

		key := snapshots.MakeKey(in.Target, in.MessageID)
		snap, err := store.Read(ctx, key)
		if err != nil {
			if errors.Is(err, snapshots.ErrNotFound) {
				metrics.SnapshotReadsTotal.WithLabelValues("miss").Inc()
				return nil, huma.Error404NotFound("snapshot not found")
			}
			if errors.Is(err, snapshots.ErrClosed) {
				return nil, huma.Error503ServiceUnavailable("snapshot store closing")
			}
			metrics.SnapshotReadsTotal.WithLabelValues("error").Inc()
			return nil, huma.Error500InternalServerError(err.Error())
		}
		metrics.SnapshotReadsTotal.WithLabelValues("hit").Inc()
		return &snapshotGetOutput{Body: snap}, nil
	})
}
