// Package subagent exposes built SDK blocks as tools in three groups.
// Spawn and join: AsTool makes a runner a spawnable subagent tool, and
// RunAll joins concurrent spawns. Mailbox: NewMailbox, SendTool, and
// InboxTool pass messages to a subagent. Block wrappers: FlowTool,
// LedgerTool, MemoryTool, RoomTool, SchedulerTool, HeartbeatTool,
// DiscoveryTool, ProviderTool, ProviderRegistryTool, TriggerTool, and
// ChannelTool expose SDK blocks as tools. See docs/packages/subagent.md
// and docs/plans/subagent.md.
package subagent
