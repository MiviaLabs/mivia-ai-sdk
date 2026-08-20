// Package agentloop runs a model until it stops asking for tools. Run
// takes a provider.Completer, a tools.Registry, and a starting message
// list. It offers the registry's tools to the model, runs the tool
// calls the model requests, appends the results as provider.RoleTool
// messages, and repeats until the model returns no tool call or a
// bound trips. RunSteerable is Run with one addition: a caller-held
// Steer handle lets another goroutine request a graceful, in-flight
// stop of the current iteration, without a hard ctx cancellation. See
// docs/plans/agentloop.md for the full contract.
package agentloop
