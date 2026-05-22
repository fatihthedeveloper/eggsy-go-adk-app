package tools

import (
	"egy-go-adk-app/agents/common/utils"

	eggsytransactionsdk "github.com/fatihthedeveloper/eggsy-transaction-sdk"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type listTransactionArgs struct {
	Email     string `json:"email" jsonschema:"The owner of the transactions to be queried, free text input, e.g. someone@gmail.com, YU7234A41"`
	StartDate string `json:"startDate" jsonschema:"the startdate of the transactions to be queried, the enddate is earlier than the startdate, so find transactions earlier than startDate and later than endDate, follows the UTC ISO format, e.g. 2006-01-02T15:04:05.000Z"`
	EndDate   string `json:"endDate" jsonschema:"the enddate of the transactions to be queried, the enddate is earlier than the startdate, so find transactions earlier than startDate and later than endDate, follows the UTC ISO format, e.g. 2006-01-02T15:04:05.000Z"`
	Category  string `json:"category" jsonschema:"the category of the transactions to be queried. This is an optional field. Just fill with empty string if the query doesn't need to be filtered by a specific category"`
}

type listTransactionResult struct {
	Transactions []createTransactionResult `json:"transactions" jsonschema:"the result list of transactions queried"`
}

func (n *NativeImplTransactionTrackerToolsBuilder) ListTransactionTool() (tool.Tool, error) {
	fn := func(context tool.Context, req listTransactionArgs) (listTransactionResult, error) {
		ctxx := utils.NewContext(context)
		currentPage, pageSize := 1, 100

		results := []createTransactionResult{}

		for {
			trxs, err := n.TrxService.List(ctxx, eggsytransactionsdk.TransactionListReq{
				Page:      currentPage,
				PageSize:  100,
				Email:     req.Email,
				Category:  req.Category,
				StartDate: req.StartDate,
				EndDate:   req.EndDate,
			})
			if err != nil {
				break
			}

			for _, res := range trxs {
				results = append(results, createTransactionResult{
					Id:              res.Id,
					Email:           res.Email,
					TransactionDate: res.TransactionDate,
					Amount:          res.Amount,
					Currency:        res.Currency,
					TransactionType: res.TransactionType,
					MerchantName:    res.MerchantName,
					Description:     res.Description,
					Category:        res.Category,
				})
			}

			if len(trxs) < pageSize {
				break
			} else {
				currentPage = currentPage + 1
			}
		}

		return listTransactionResult{
			Transactions: results,
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "list_transaction_tool",
		Description: "List transactions with several filter parameters",
	}, fn)
	if err != nil {
		return nil, err
	}

	return newTool, nil
}
