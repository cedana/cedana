<!-- Implement tasks from a task list. 
    Usage: "@implement_tasks.md for @feature_tasklist.md"
 -->
 
<instructions>
Proceed with sequentially implementing each task in the task list. Maintain the task list at each step according to instructions in @task-list.mdc. 

# Before starting **each** task: 
1. Consider whether you have >95% confidence that you know what to build. Proceed if you have 95% confidence.
2. If that confidence is below 95%, ask follow-up questions until you have that confidence. 
3. Consolidate your questions into a single numbered list to make answer easier.
4. Update the @tasklist.md task list based the answers you receive, before proceeding with implementing the task.

# After completing each parent task (not subtask):
1. Commit your changes to the git repository with a meaningful, concise commit message (less than 80 characters) according to @git.mdc. **ALWAYS commit as "Agent <agent@cedana.ai>"**

# Important
- Do not change the task list file structure or delete the task list file. The task list file should **ALWAYS** have the following sections: 
```markdown
## Completed Tasks
## In Progress Tasks
## Future Tasks
## Implementation Plan
### Relevant Files
</instructions>

Task List:
