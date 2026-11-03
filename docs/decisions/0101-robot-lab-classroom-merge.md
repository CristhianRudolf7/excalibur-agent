# 0101: Combine Robot Lab with Classroom and supervised robot access

The merged daemon initializes Classroom, supervised Robot and Robot Lab together.
Assistant turns retain per-message app context, contextual tool and skill
selection, artifact recording and completion events, and context propagation to
delegated work. Robot Lab adds its enabled tool to that same runner. JSON-RPC
methods, event registries and client schemas retain the union of both branches.
This preserves the application and execution boundaries in Sections 3.5, 13.4,
14.8 and 22.3.

The web shell exposes Classroom, Robot and Robot Lab in both sidebar layouts.
The TanStack router generator produces the combined route tree.

Classroom and Robot Lab remain deferred outside eval. Adding `robot_lab` to the
combined default catalog changes the tool-name pin from
`8fce82d7bd4e6632e159c7c7d69fcbb505087e905d385494a8462e1d09148ae4`
to `96daa69555fed7ce799dbd7cc6fa973fe5278eff85bf3e0c84fe807e6d2921ff`.
The fixed prefix remains
`13e36f12dbbd571bb406e6fdaa55633ed619bf8724d03a1716e443a8faf61557`.
The Section 23.2 eval profile remains
`4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`.
Provider adapters, model wiring, MCP/ACP and Harbor are unchanged.

`TestClassroomTurnRetainsRobotLabAndArtifactDelivery` uses a simulated model and
tool to verify that one Classroom-context turn offers both application tools and
the skill loader, carries the message context into the model, and publishes one
created/ready artifact pair with its conversation and tool-call provenance.
Existing feature suites cover the individual services, RPC methods, deferred
execution, delegation context and eval exclusions.
