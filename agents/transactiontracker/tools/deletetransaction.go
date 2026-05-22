package tools

import (
	"egy-go-adk-app/agents/common/utils"

	eggsytransactionsdk "github.com/fatihthedeveloper/eggsy-transaction-sdk"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type deleteTransactionArgs struct {
	Id string `json:"id" jsonschema:"the id of the transaction to be deleted"`
}

type deleteTransactionResult struct {
	Id string `json:"id" jsonschema:"the id of the transaction deleted"`
}

func (n *NativeImplTransactionTrackerToolsBuilder) DeleteTransactionTool() (tool.Tool, error) {
	fn := func(ctx tool.Context, args deleteTransactionArgs) (deleteTransactionResult, error) {
		ctxx := utils.NewContext(ctx)
		err := n.TrxService.Delete(ctxx, eggsytransactionsdk.TransactionGetReq{
			Id: args.Id,
		})
		if err != nil {
			return deleteTransactionResult{}, err
		}

		return deleteTransactionResult{
			Id: args.Id,
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "delete_transaction_tool",
		Description: "Deletes a transaction",
	}, fn)
	if err != nil {
		return nil, err
	}

	return newTool, nil
}
