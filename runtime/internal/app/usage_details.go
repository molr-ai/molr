package app

import (
	"context"
	"math"
	"sort"
	"strings"

	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Read only accounting metadata, never tool arguments/results or file names.
func usageDetails(ctx context.Context, store persistence.Store, owner core.ID) ([]accountview.ToolUsage, []accountview.StorageCategory, int64, error) {
	tools := []accountview.ToolUsage{}
	attachments := []accountview.StorageCategory{}
	var total int64
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Select[struct{ Action, State string }](ctx, tx.Store(), "agent.operations", persistence.Query{
			Unordered: true, Fields: []string{"Action", "State"}, Where: []persistence.Filter{
				persistence.Eq("OwnerID", owner), {Field: "Action", Op: "!=", Value: "model.generate"}, {Field: "State", Op: "!=", Value: "prepared"},
			},
		})
		if err != nil {
			return err
		}
		byTool := map[string]*accountview.ToolUsage{}
		for _, row := range rows {
			v := byTool[row.Action]
			if v == nil {
				v = &accountview.ToolUsage{Name: row.Action}
				byTool[row.Action] = v
			}
			v.Calls++
			switch row.State {
			case "succeeded":
				v.Succeeded++
			case "failed":
				v.Failed++
			case "canceled":
				v.Canceled++
			case "running":
				v.Active++
			default:
				v.Unknown++
			}
		}
		for _, v := range byTool {
			tools = append(tools, *v)
		}
		sort.Slice(tools, func(i, j int) bool {
			if tools[i].Calls != tools[j].Calls {
				return tools[i].Calls > tools[j].Calls
			}
			return tools[i].Name < tools[j].Name
		})
		files, err := persistence.Select[struct {
			Size      int64
			MediaType string
		}](ctx, tx.Store(), "storage.files", persistence.Query{
			Unordered: true, Fields: []string{"Size", "MediaType"}, Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("State", "ready")},
		})
		if err != nil {
			return err
		}
		byType := map[string]*accountview.StorageCategory{}
		for _, row := range files {
			if row.Size < 0 || row.Size > math.MaxInt64-total {
				return core.NewError(core.Internal, "usage overflow")
			}
			total += row.Size
			kind := strings.ToLower(strings.SplitN(row.MediaType, "/", 2)[0])
			switch kind {
			case "image", "audio", "video", "text":
			default:
				kind = "other"
			}
			v := byType[kind]
			if v == nil {
				v = &accountview.StorageCategory{Kind: kind}
				byType[kind] = v
			}
			v.Bytes += row.Size
			v.Files++
		}
		for _, kind := range []string{"image", "audio", "video", "text", "other"} {
			if v := byType[kind]; v != nil {
				attachments = append(attachments, *v)
			}
		}
		return nil
	})
	return tools, attachments, total, err
}
