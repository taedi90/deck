package askhooks

import "github.com/taedi90/deck/internal/askintent"

type Hooks struct {
	PreClassify  func(prompt string) string
	PostClassify func(decision askintent.Decision) askintent.Decision
}

func Default() Hooks {
	return Hooks{
		PreClassify: func(prompt string) string { return prompt },
		PostClassify: func(decision askintent.Decision) askintent.Decision {
			return decision
		},
	}
}
