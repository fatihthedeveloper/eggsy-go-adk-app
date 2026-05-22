package tools

import (
	"egy-go-adk-app/agents/common/utils"

	eggsyaccountsdk "github.com/fatihthedeveloper/eggsy-account-sdk"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type createAccountByEmailArgs struct {
	Email string `json:"email" jsonschema:"The email for expense tracker account creation, can be any string. e.g. fatih@gmail.com, 42A7UT998"`
}

type createAccountByEmailResult struct {
	Email string `json:"email" jsonschema:"The email set for the created expense tracker account, can be any string. e.g. fatih@gmail.com, 42A7UT998"`
}

func (n *NativeImplTransactionTrackerToolsBuilder) CreateAccountByEmailTool() (tool.Tool, error) {
	fn := func(ctx tool.Context, args createAccountByEmailArgs) (createAccountByEmailResult, error) {
		ctxx := utils.NewContext(ctx)

		res, err := n.AccService.Create(ctxx, eggsyaccountsdk.AccountCreateReq{
			Email:            args.Email,
			WithReturnSecret: false,
		})

		if err != nil {
			return createAccountByEmailResult{}, err
		}

		return createAccountByEmailResult{
			Email: res.Email,
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "create_account_by_email_tool",
		Description: "Creates an account using the given email input.",
	}, fn)

	if err != nil {
		return nil, err
	}

	return newTool, nil
}
