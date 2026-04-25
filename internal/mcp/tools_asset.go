package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerAssetTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("asset_list",
		mcp.WithDescription("List all fixed assets in the asset register."),
	), assetListHandler(deps))

	s.AddTool(mcp.NewTool("asset_detail",
		mcp.WithDescription("Full detail for a single fixed asset."),
		mcp.WithNumber("asset_id",
			mcp.Description("Fortnox asset ID."),
			mcp.Required(),
		),
	), assetDetailHandler(deps))
}

// --- asset_list ---

func assetListHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		assets, err := deps.AssetReg.Assets(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch assets: %v", err)), nil
		}

		return jsonResult(assets)
	}
}

// --- asset_detail ---

func assetDetailHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		assetID, err := req.RequireInt("asset_id")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("asset_id required: %v", err)), nil
		}

		asset, err := deps.AssetReg.AssetDetail(ctx, deps.TenantID, assetID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch asset detail: %v", err)), nil
		}

		return jsonResult(asset)
	}
}
