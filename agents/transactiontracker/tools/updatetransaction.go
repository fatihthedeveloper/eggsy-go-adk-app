package tools

import (
	"egy-go-adk-app/agents/common/utils"

	eggsytransactionsdk "github.com/fatihthedeveloper/eggsy-transaction-sdk"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type updateTransactionArgs struct {
	Id              string `json:"id" jsonschema:"the id of the transaction to be updated, usually in form of UUID, e.g. b0cec0f2-5dc9-43cf-8e83-cfb261217208"`
	Email           string `json:"email" jsonschema:"The email to own this transaction, allow free text input, e.g. someone@gmail.com, YU7234A41"`
	Amount          int    `json:"amount" jsonschema:"The amount of the transaction"`
	Currency        string `json:"currency" jsonschema:"the currency of the transaction, e.g. IDR, MYR, USD, SGD"`
	TransactionDate string `json:"transactionDate" jsonschema:"The transaction date in UTC ISO format, e.g. 2006-01-02T15:04:05.000Z"`
	TransactionType string `json:"transactionType" jsonschema:"transaction type of transaction, only allow 'expense' or 'income'"`
	MerchantName    string `json:"merchantName" jsonschema:"the merchant name of the transaction, if a merchant name is not found, just input UNKNOWN"`
	Description     string `json:"description" jsonschema:"additional information related to the transaction"`
	Category        string `json:"category" jsonschema:"the category of the transaction, e.g. Food & Drinks, Health, Home Bills"`
}

type updateTransactionResult struct {
	Id              string `json:"id" jsonschema:"the id of the transaction, usually in form of UUID, e.g. b0cec0f2-5dc9-43cf-8e83-cfb261217208"`
	Email           string `json:"email" jsonschema:"The email to own this transaction, allow free text input, e.g. someone@gmail.com, YU7234A41"`
	TransactionDate string `json:"transactionDate" jsonschema:"The transaction date in UTC ISO format, e.g. 2006-01-02T15:04:05.000Z"`
	Amount          int    `json:"amount" jsonschema:"The amount of the transaction"`
	Currency        string `json:"currency" jsonschema:"the currency of the transaction, e.g. IDR, MYR, USD, SGD"`
	TransactionType string `json:"transactionType" jsonschema:"transaction type of transaction, only allow 'expense' or 'income'"`
	MerchantName    string `json:"merchantName" jsonschema:"the merchant name of the transaction, if a merchant name is not found, just input UNKNOWN"`
	Description     string `json:"description" jsonschema:"additional information related to the transaction"`
	Category        string `json:"category" jsonschema:"the category of the transaction, e.g. Food & Drinks, Health, Home Bills"`
}

func (n *NativeImplTransactionTrackerToolsBuilder) UpdateTransactionTool() (tool.Tool, error) {
	fn := func(context tool.Context, req updateTransactionArgs) (updateTransactionResult, error) {
		ctxx := utils.NewContext(context)
		res, err := n.TrxService.Update(ctxx, eggsytransactionsdk.TransactionUpdateReq{
			Id:              req.Id,
			Amount:          req.Amount,
			Currency:        req.Currency,
			TransactionType: req.TransactionType,
			MerchantName:    req.MerchantName,
			Description:     req.Description,
			Category:        req.Category,
			TransactionDate: req.TransactionDate,
		})
		if err != nil {
			return updateTransactionResult{}, err
		}

		return updateTransactionResult{
			Id:              res.Id,
			Email:           res.Email,
			TransactionDate: res.TransactionDate,
			Amount:          res.Amount,
			Currency:        res.Currency,
			TransactionType: res.TransactionType,
			MerchantName:    res.MerchantName,
			Description:     res.Description,
			Category:        res.Category,
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "update_transaction_tool",
		Description: "Updates a transaction record",
	}, fn)
	if err != nil {
		return nil, err
	}

	return newTool, nil
}
