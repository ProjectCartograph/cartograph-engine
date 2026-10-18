# Find the critical path and fix a loop

This guide shows you how to read the chain of components that sets the last date, and how to break a loop where work depends on itself. The example is the cooperative's *Quality Check Rollout*.

## Find the critical path

1. In the rail, choose **Projects** and select *Quality Check Rollout*.
2. Open the **Context** step. The map is beside it. If you do not see it, open the **Map** tab.
3. Beside **Show**, choose **Dependencies**. The map draws the projects and programmes that depend on one another.
4. Read the line under the map. It starts *Critical path:* and names the chain, for example *Quality Improvement Programme depends on Quality Check Rollout depends on Market Access Programme*, with its length in months.

On the map, the work on that chain carries a stopwatch (its tooltip reads *On the critical path: a delay here delays everything that depends on it.*). The work that more others depend on than anything else carries a share mark (*Most depended on: more work depends on it than on anything else.*). The same marks appear on each name under **Depends on** and **Used by**.

If the line reads *No critical path yet: none of the linked work has a schedule to measure it by.*, give the milestones in the linked projects dates in their **Schedule** step. A programme has no dates of its own.

## Find a loop

A loop shows in three places:

- The line under the map reads *Loop:* and names the chain in order, ending where it began.
- The work in the loop carries a red mark (*In a loop: it depends on itself through its components.*), and the link between them is red and dashed.
- In the **Checks** tab, a check reads *Depends on itself* and names the chain.

## Fix a loop

1. In the line under the map, find the two pieces of work that close the loop.
2. In **Depends on**, find the one that should not wait on the other.
3. Press the cross beside it (its tooltip reads *Stop depending on* and the name).
4. The red mark goes. The line under the map reads *Critical path:* again, and the *Depends on itself* check is gone.

If the cross is on the other piece of work's page, open that page and remove it there. Each project or programme changes only its own list.

## See also

- [Add a project or programme as a component](add-a-project-or-programme-as-a-component.md)
- [Split a large project into components](split-a-large-project-into-components.md)
- [About components and large projects](../explanation/about-components-and-large-projects.md)
- [About checks](../explanation/about-checks.md)
