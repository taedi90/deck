package workflowexpr

import "github.com/taedi90/deck/internal/workflowcontract"

type Contract struct {
	Language   string
	Namespaces []string
}

func PublicContract() Contract {
	return Contract{
		Language:   workflowcontract.WhenLanguage,
		Namespaces: workflowcontract.WhenNamespaces(),
	}
}
