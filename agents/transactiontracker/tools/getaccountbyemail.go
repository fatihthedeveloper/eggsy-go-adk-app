package tools

import (
	"egy-go-adk-app/agents/common/utils"

	eggsyaccountsdk "github.com/fatihthedeveloper/eggsy-account-sdk"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type getAccountByEmailArgs struct {
	Email string `json:"email" jsonschema:"The identifier to check if an account exists by"`
}

type getAccountByEmailResult struct {
	AccountExists bool `json:"accountExists" jsonschema:"true if an account with this email already exists, false if it does not"`
}

func (n *NativeImplTransactionTrackerToolsBuilder) GetAccountByEmailTool() (tool.Tool, error) {
	fn := func(ctx tool.Context, args getAccountByEmailArgs) (getAccountByEmailResult, error) {
		ctxx := utils.NewContext(ctx)

		res, err := n.AccService.Exists(ctxx, eggsyaccountsdk.AccountExistsReq{
			Email: args.Email,
		})
		if err != nil {
			return getAccountByEmailResult{}, err
		}

		return getAccountByEmailResult{
			AccountExists: res.Exists,
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "get_account_exists_by_email_tool",
		Description: "Checks if an account exists with the given email. Call this tool ONCE per request. If accountExists is true, proceed directly to the user's request. If accountExists is false, call create_account_by_email_tool first, then proceed.",
	}, fn)

	if err != nil {
		return nil, err
	}

	return newTool, nil
}
