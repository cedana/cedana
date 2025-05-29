<context>
You will be given a complete conversation history that contains: 
- User & Assistant messages
- File diffs and code edits 
- Terminal output 
- Environment details 
- Other logs or metadata 

- Summarize this conversation so it can be used as a comprehensive “starting context” for a new session, preserving the relevant design/development decisions and key information while omitting unnecessary details.
- Additionally, provide a clear summary of the **final steps or messages** from the conversation, so we know exactly where we left off and what was being worked on if the conversation ended mid-task.
</context>


<instructions>
Review the conversation history and produce a comprehensive handoff summary (handoff_summary.md) that includes the following information:
1.  **Project/Task Overview**
    - High-level description of what the project or conversation is about.
    - Main objectives or goals.
2.  **Key Design & Development Decisions**
    - Important architectural or UI/UX choices.
    - Technical approaches discussed or chosen.
3.  **Notable Code Changes or Edits**
    - Summarize what changed, why it changed, and any important implications or next steps.
    - Omit lengthy diffs or logs. Provide a concise bullet list of the main edits.
4.  **Issues Encountered & Resolutions**
    - Briefly note any errors, bugs, or challenges that were addressed and how they were resolved.
5.  **Open Questions / Next Steps**
    - Unresolved items, pending tasks, or suggestions for future action.
6.  **Immediate Context of the Last Few Steps**
    - A succinct recap of the final messages or actions taken just before the conversation ended.
    - Clearly outline the state of any in-progress tasks or code changes to avoid confusion if the session resumes.
7.  **Last Known Error (if applicable)**


Also, include the following information:
1.  Summarize the most recent/current development objective.
2.  Include a list of files (with pathnames) that should be read to get up to speed on the current development objective.
3.  Summarize most recent architectural decisions.
</instructions>


<output>
- **Exclude large code snippets**: If specific code is crucial, summarize rather than copy entire blocks.
- **Maintain clarity**: Present your summary in well-organized, easy-to-read bullet points or short paragraphs.
</output>



