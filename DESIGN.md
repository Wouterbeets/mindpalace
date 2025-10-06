# MindPalace Front-End Transparency Design

## Overview
MindPalace aims for radical transparency in how the AI backend operates, setting it apart from other AI tools. The front-end will visualize both the "machinery under the hood" (events) and the "results above ground" (current state). This design embraces eventsourcing: events are the immutable source of truth, state is derived.

- **Underground Layer**: Events spiraling down into infinity, showing the AI's "magic" in action (e.g., task creation, AI decisions, audio transcription).
- **Above Ground Layer**: Current state (e.g., tasks, notes) as usable 3D objects, updated via deltas or snapshots.

This ensures users see *how* and *with what data* the AI works, while keeping the interface usable. Focus on core functionality; no UI toggles or advanced features yet.

## Goals
- **Transparency**: Visualize event streams for explainability.
- **Usability**: State layer remains clean and interactive.
- **Scalability**: Separate layers prevent overload.
- **Elegance**: Easy to reason about—events drive underground, state drives above ground.
- **Lean**: No new event metadata; use existing event data.

## Architecture
- **Backend (Go)**: Eventsourced aggregates emit events. `Broadcast3DDelta` for live state deltas. New `GetCurrent3DState` for full state snapshots.
- **Front-End (Godot)**: Two layers:
  - Above Ground: 3D scene with current objects (tasks as cubes).
  - Underground: Event visualization (e.g., spiraling particles/text).
- **Communication**: WebSocket for events, deltas, and snapshots. Keystroke endpoint for testing.
- **Sync**: On reconnect, send full state instantly (no unfolding). Live events unfold in underground.

## Signals Over WebSocket
Instead of deltas, use "signals": Messages containing a list of actions for Godot to execute (like applying events in the backend), plus a current state summary as seen by the backend (e.g., list of task IDs with position and color, after actions applied). Godot has knowledge of the system and applies actions to maintain state.

Signal structure:
- `type`: "signal"
- `actions`: Array of actions (create, delete, update, animate, move_to, change_color, etc.), e.g.:
  ```json
  [
    {"type": "create", "node_id": "task_1", "node_type": "MeshInstance3D", "properties": {...}},
    {"type": "animate", "node_id": "task_1", "animation": "spiral_down"},
    {"type": "move_to", "node_id": "task_1", "position": [0,0,20]}
  ]
  ```
- `state_summary`: Object with current state (e.g., {"tasks": [{"id": "task_1", "position": [0,0,20], "color": [1,0,0]}]})
- `aggregate`: e.g., "taskmanager"
- `event_id`: ID of the triggering event (optional)

For reconnect: Send full state signal with all creates and complete state_summary.
For live: Send incremental signals with actions and updated state_summary.

This aligns with 2 layers: Actions drive underground animations/events, state_summary drives above ground objects.

## Implementation Plan
Incremental TDD approach: Write tests first, fail, implement, integrate, commit.

### Phase 1: Core Infrastructure
1. Extend `ThreeDUIBroadcaster` with `GetCurrent3DState() []DeltaAction`.
2. Implement in `TaskAggregate`: Generate full state actions from `a.Tasks`.

### Phase 2: Godot Layers
3. Add underground scene (spiraling events).
4. Modify message handling for deltas (incremental) vs. snapshots (full rebuild).

### Phase 3: Sync & Testing
5. Update `sendFullState` for instant snapshots on reconnect.
6. Add API route for runtime visibility (e.g., /debug/state).
7. Godot sends debug data over WS (e.g., current object count).
8. Integration tests: Keystroke endpoint to trigger events, verify layers.

## Testing Strategy
- **Unit Tests**: Aggregate methods, Godot message parsing.
- **Integration Tests**: Full flow via keystroke endpoint (e.g., send "create task", verify underground spirals and above ground updates).
- **Visibility**: New API route `/debug/godot-state` returns Godot's reported state. Godot sends periodic WS messages with metrics (e.g., {"type": "debug", "object_count": 5}).
- **HARCODE Style**: Write failing tests, implement minimally, green, commit. One feature at a time.

## Risks & Mitigations
- **Overload**: Default to underground visible but simple.
- **Performance**: Limit event history; batch sends.
- **Complexity**: Keep layers separate; test incrementally.

## Todolist
- [x] Create DESIGN.md (this doc)
- [x] Validate design with team
- [x] Phase 1: Extend ThreeDUIBroadcaster
- [x] Phase 1: Implement GetCurrent3DState in TaskAggregate
- [x] Phase 3: Update sendFullState
- [ ] Phase 2: Godot underground scene
- [ ] Phase 2: Message handling split
- [ ] Phase 3: Add /debug API route
- [ ] Phase 3: Godot debug WS messages
- [ ] Phase 3: Integration tests with keystrokes