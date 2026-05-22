package tools

import (
	eggsyaccountsdk "github.com/fatihthedeveloper/eggsy-account-sdk"
	eggsytransactionsdk "github.com/fatihthedeveloper/eggsy-transaction-sdk"
	"google.golang.org/adk/tool"
)

type TransactionTrackerToolsBuilder interface {
	CreateAccountByEmailTool() (tool.Tool, error)
	GetAccountByEmailTool() (tool.Tool, error)
	CreateTransactionTool() (tool.Tool, error)
	ListTransactionTool() (tool.Tool, error)
	UpdateTransactionTool() (tool.Tool, error)
	DeleteTransactionTool() (tool.Tool, error)
	GetTransactionTool() (tool.Tool, error)
}

type NativeImplTransactionTrackerToolsBuilder struct {
	TrxService eggsytransactionsdk.TransactionService
	AccService eggsyaccountsdk.AccountService
}
