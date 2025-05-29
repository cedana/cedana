<!-- A Handoff document (handoff_document) and Task List document (task_list_document) should be provided. (Request these from the user if they are not provided.) -->

<goa
- Efficiently grasp the project's purpose, current progress, key decisions, and pinpoint the immediate next task to continue development based on a provided handoff_document and task_list_document.
- Focus on the Next Task: Your primary goal is to become productive on the *next immediate task*. You can learn about other parts of the codebase as neede
</goal>

<instructions>
Read the following documents:

plugins/*
pkg/*
cmd/*
docs/*
internal/*
main.go

<handoff_document>
{{handoff_document}}
</handoff_document
ask_list_document>
{{task_list_document}}
</task_list_document
<steps>
1.  Read the Handoff Document to understand the following:
    - Project/Task Overview
    - Key Design & Development Decisions
    - Notable Code Changes or Edits
    - Issues Encountered & Resolutions
    - Open Questions / Next Steps
    - Immediate Context of the Last Few Steps
    - Files to Review for Onboarding

2.  Read the Task List Document and verify the next task:
    - Cross-reference the "Next Steps" identified from handoff_document with the task list. Look for tasks marked as "In Progress" or the next unchecked item in the "Future Tasks" or main task sections.
    - Read the description of the identified next task to understand its specific requirements and deliverables.

3.  Read the files in the "Files to Review" section of the handoff_document to understand the codebase and the next task.
    - Concentrate on the modules, functions, or sections directly related to the task at hand. 
    - Look for comments, function signatures, and overall structure to quickly grasp the purpose of different code blocks.

4.  Formulate your plan and ask questions:
    - Outline the steps you'll take to complete the next task.
    - Identify dependencies and any potential issues.
    - If anything is unclear after reviewing handoff_document, task_list_document, and relevant code, now is the time to formulate specific questions.

5.  Ready to Contribute!
    - You should now have a solid understanding of the project's current state and be prepared to start working on the next task.
</instructions>

