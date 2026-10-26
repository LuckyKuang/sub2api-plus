package service

const RuntimeRecoveryGenerationExtraKey = "runtime_recovery_generation"

func runtimeRecoveryGeneration(account *Account) string {
	if account == nil {
		return ""
	}
	generation, _ := account.Extra[RuntimeRecoveryGenerationExtraKey].(string)
	return generation
}
