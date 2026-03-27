package workflowissues

import "sort"

const (
	CodeDuplicateStepID              Code = "duplicate_step_id"
	CodeDuplicatePhaseName           Code = "duplicate_phase_name"
	CodeMissingEntryScenario         Code = "missing_entry_scenario"
	CodeMissingPrepareApplyStructure Code = "missing_prepare_apply_structure"
	CodeMissingRoleSelector          Code = "missing_role_selector"
	CodeMissingArtifactConsumer      Code = "missing_artifact_consumer"
	CodeInvalidImportTarget          Code = "invalid_import_target"
	CodeUnsupportedRoleStepKind      Code = "unsupported_role_step_kind"
	CodeAmbiguousJoinContract        Code = "ambiguous_join_contract"
	CodeArtifactContractGap          Code = "artifact_contract_gap"
	CodeWeakVerificationStaging      Code = "weak_verification_staging"
	CodeRoleCardinalityGap           Code = "role_cardinality_gap"
	CodeTopologyFidelityGap          Code = "topology_fidelity_gap"
	CodeWorkerJoinFanoutGap          Code = "worker_join_fanout_gap"
	CodeAskUnclassifiedCriticFinding Code = "ask_unclassified_critic_finding"
)

var registry = map[Code]Spec{
	CodeDuplicateStepID:              {Code: CodeDuplicateStepID, Class: ClassSemantic, DefaultSeverity: SeverityBlocking, Summary: "Step ids must be unique across the workflow.", Details: "Every step id must be unique across top-level steps and steps nested under phases.", PromptHint: "Rename duplicate step ids with role- or phase-specific prefixes instead of reusing the same id."},
	CodeDuplicatePhaseName:           {Code: CodeDuplicatePhaseName, Class: ClassSemantic, DefaultSeverity: SeverityBlocking, Summary: "Phase names should be unique within a workflow.", Details: "Repeated phase names make execution and diagnostics ambiguous."},
	CodeMissingEntryScenario:         {Code: CodeMissingEntryScenario, Class: ClassContract, DefaultSeverity: SeverityBlocking, Summary: "A viable entry scenario must be present.", Details: "Plans that generate workflows must identify one planned scenario file as the entry scenario."},
	CodeMissingPrepareApplyStructure: {Code: CodeMissingPrepareApplyStructure, Class: ClassContract, DefaultSeverity: SeverityBlocking, Summary: "Prepare/apply workflows need both prepare and apply files.", Details: "When the plan targets prepare+apply, it must include a prepare workflow and an apply entry scenario."},
	CodeMissingRoleSelector:          {Code: CodeMissingRoleSelector, Class: ClassContract, DefaultSeverity: SeverityBlocking, Summary: "Multi-role plans need an execution role selector.", Details: "Multi-node or role-aware apply flows need a role selector or equivalent branching model."},
	CodeMissingArtifactConsumer:      {Code: CodeMissingArtifactConsumer, Class: ClassContract, DefaultSeverity: SeverityBlocking, Summary: "Artifact-dependent plans need a viable consumer path.", Details: "If prepare stages artifacts, apply must have a concrete consumer path for those artifacts."},
	CodeInvalidImportTarget:          {Code: CodeInvalidImportTarget, Class: ClassStructural, DefaultSeverity: SeverityBlocking, Summary: "Imports must point at valid component paths.", Details: "Phase imports must resolve under workflows/components/."},
	CodeUnsupportedRoleStepKind:      {Code: CodeUnsupportedRoleStepKind, Class: ClassSemantic, DefaultSeverity: SeverityBlocking, Summary: "Selected step kind is not supported for the current workflow role.", Details: "Prepare and apply have different supported step kinds."},
	CodeAmbiguousJoinContract:        {Code: CodeAmbiguousJoinContract, Class: ClassQuality, DefaultSeverity: SeverityAdvisory, DefaultRecoverable: true, Summary: "Join handoff contracts should be explicit and unambiguous.", Details: "Worker join publication and consumption should reference one authoritative join handoff contract.", PromptHint: "Use one authoritative join handoff contract and make the producer/consumer path explicit."},
	CodeArtifactContractGap:          {Code: CodeArtifactContractGap, Class: ClassQuality, DefaultSeverity: SeverityMissingContract, DefaultRecoverable: true, Summary: "Artifact contracts should be explicit enough for generation.", Details: "Artifact producers, consumers, and bundle layout should be explicit when apply depends on prepared assets."},
	CodeWeakVerificationStaging:      {Code: CodeWeakVerificationStaging, Class: ClassQuality, DefaultSeverity: SeverityAdvisory, DefaultRecoverable: true, Summary: "Verification staging should match topology and execution flow.", Details: "Final verification should not race worker joins or run before required state is ready."},
	CodeRoleCardinalityGap:           {Code: CodeRoleCardinalityGap, Class: ClassQuality, DefaultSeverity: SeverityMissingContract, DefaultRecoverable: true, Summary: "Role cardinality should be explicit for multi-node plans.", Details: "The plan should describe how many control-plane and worker nodes are expected."},
	CodeTopologyFidelityGap:          {Code: CodeTopologyFidelityGap, Class: ClassQuality, DefaultSeverity: SeverityAdvisory, DefaultRecoverable: true, Summary: "Topology-specific execution details should be explicit.", Details: "Role mapping and node identity resolution should match the requested topology."},
	CodeWorkerJoinFanoutGap:          {Code: CodeWorkerJoinFanoutGap, Class: ClassQuality, DefaultSeverity: SeverityAdvisory, DefaultRecoverable: true, Summary: "Worker join fan-out should be modeled explicitly.", Details: "Worker join flows should make per-node execution and completion behavior explicit."},
	CodeAskUnclassifiedCriticFinding: {Code: CodeAskUnclassifiedCriticFinding, Class: ClassQuality, DefaultSeverity: SeverityAdvisory, DefaultRecoverable: true, Summary: "Critic finding did not map to a known shared issue code.", Details: "Temporary compatibility code for legacy or unclassified critic text."},
}

var criticCodes = map[Code]bool{
	CodeMissingEntryScenario:         true,
	CodeMissingPrepareApplyStructure: true,
	CodeMissingRoleSelector:          true,
	CodeMissingArtifactConsumer:      true,
	CodeAmbiguousJoinContract:        true,
	CodeArtifactContractGap:          true,
	CodeWeakVerificationStaging:      true,
	CodeRoleCardinalityGap:           true,
	CodeTopologyFidelityGap:          true,
	CodeWorkerJoinFanoutGap:          true,
	CodeAskUnclassifiedCriticFinding: true,
}

func SpecFor(code Code) (Spec, bool) {
	spec, ok := registry[code]
	return spec, ok
}

func MustSpec(code Code) Spec {
	spec, ok := registry[code]
	if !ok {
		panic("unknown workflow issue code: " + string(code))
	}
	return spec
}

func IsRegistered(code Code) bool {
	_, ok := registry[code]
	return ok
}

func IsSupportedCriticCode(code Code) bool {
	return criticCodes[code]
}

func SupportedCriticCodes() []Code {
	out := make([]Code, 0, len(criticCodes))
	for code := range criticCodes {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func SupportedCriticCodeStrings() []string {
	codes := SupportedCriticCodes()
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		out = append(out, string(code))
	}
	return out
}

func SemanticRuleSummaries() []string {
	return []string{
		MustSpec(CodeDuplicateStepID).Summary,
		"A workflow must define at least one of phases or steps.",
		"A workflow cannot define both top-level phases and top-level steps at the same time.",
	}
}
