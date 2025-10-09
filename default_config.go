package main

func getDefaultConfigContent() string {
	return `# goscribe configuration file
# All built-in post-processing actions for audio transcription

# OpenAI API Key (optional - can also be passed via -k flag)
# If set here, you don't need to provide -k flag every time
openai_api_key: ""

post_actions:
  - id: "openai-meeting-summary"
    name: "Smart Meeting Summary"
    description: "AI-powered comprehensive meeting summary with key decisions and action items"
    type: "openai"
    prompt: |
      Analyze this meeting transcript and create a comprehensive summary with the following sections:

      1. **Key Decisions Made**
      2. **Action Items** (with responsible parties if mentioned)
      3. **Important Discussion Points**
      4. **Next Steps**

      Format the output with clear headings and bullet points for easy reading.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1500

  - id: "openai-action-items"
    name: "Action Items Extractor"
    description: "Extract and organize all action items, tasks, and assignments"
    type: "openai"
    prompt: |
      Extract all action items, tasks, deadlines, and assignments from this transcript. For each item, identify:

      - The specific task or action required
      - Who is responsible (if mentioned)
      - Any deadlines or timeframes mentioned
      - Priority level (if indicated)

      Format as a prioritized task list with checkboxes.
    model: "gpt-3.5-turbo"
    temperature: 0.2
    max_tokens: 1000

  - id: "openai-executive-brief"
    name: "Executive Brief"
    description: "Concise executive summary for leadership review"
    type: "openai"
    prompt: |
      Create a concise executive summary of this transcript suitable for leadership review. Focus on:

      - Strategic decisions and their business impact
      - Financial implications or budget discussions
      - Risk factors or opportunities identified
      - Key performance metrics or outcomes
      - Critical next steps requiring leadership attention

      Keep it under 300 words and use business-appropriate language.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 800

  - id: "openai-key-insights"
    name: "Key Insights"
    description: "Identify important insights, conclusions, and strategic points"
    type: "openai"
    prompt: |
      Identify and extract the most important insights, conclusions, and strategic points from this transcript. Focus on:

      - Novel ideas or innovative solutions discussed
      - Important data points or metrics mentioned
      - Strategic implications for the business
      - Risk factors or challenges identified
      - Opportunities for improvement or growth

      Present as numbered insights with brief explanations.
    model: "gpt-3.5-turbo"
    temperature: 0.4
    max_tokens: 1200

  - id: "openai-qa-format"
    name: "Q&A Generator"
    description: "Convert transcript into structured question and answer format"
    type: "openai"
    prompt: |
      Convert this transcript into a comprehensive Q&A format that captures all important questions asked and answers provided. Include:

      - All direct questions and their answers
      - Implied questions from the discussion
      - Key topics addressed even if not explicitly asked

      Format with clear Q: and A: markers for easy reading.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 2000

  - id: "openai-tech-meeting"
    name: "Technical Meeting Summary"
    description: "Summarize technical discussions with architecture decisions and implementation details"
    type: "openai"
    prompt: |
      Analyze this technical meeting transcript and provide:

      1. **Technical Decisions Made** (architecture, technology choices, design patterns)
      2. **Implementation Details** (specific approaches, libraries, tools mentioned)
      3. **Technical Challenges Identified** (blockers, performance issues, technical debt)
      4. **Code/System Changes Required**
      5. **Technical Action Items** (with owners if mentioned)
      6. **Open Technical Questions**

      Use technical terminology appropriately and include any code snippets, API endpoints, or system components mentioned.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 2000

  - id: "openai-one-on-one"
    name: "1:1 Meeting Notes"
    description: "Structure manager/employee 1:1 discussions with feedback and growth areas"
    type: "openai"
    prompt: |
      Structure this 1:1 meeting transcript into clear sections:

      1. **Performance Feedback** (positive and constructive)
      2. **Goals & Objectives Discussed**
      3. **Career Development Topics** (growth opportunities, skills to develop)
      4. **Challenges/Blockers Mentioned**
      5. **Support Needed from Manager**
      6. **Action Items for Employee**
      7. **Action Items for Manager**
      8. **Follow-up Items for Next 1:1**

      Maintain a professional and constructive tone throughout.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1500

  - id: "openai-hr-meeting"
    name: "HR Meeting Summary"
    description: "Summarize HR discussions with policy updates and employee matters"
    type: "openai"
    prompt: |
      Summarize this HR meeting focusing on:

      1. **Policy Updates or Changes** (what changed and why)
      2. **Employee Benefits/Compensation Discussion**
      3. **Performance/Development Topics**
      4. **Compliance or Legal Matters Mentioned**
      5. **Next Steps & Required Documentation**
      6. **Important Dates/Deadlines**
      7. **Questions to Follow Up On**

      Maintain confidentiality-appropriate language and focus on actionable information.
    model: "gpt-3.5-turbo"
    temperature: 0.2
    max_tokens: 1200

  - id: "openai-project-kickoff"
    name: "Project Kickoff Summary"
    description: "Capture project objectives, scope, timeline, and team structure"
    type: "openai"
    prompt: |
      Create a comprehensive project kickoff summary from this transcript:

      1. **Project Overview** (goals, objectives, success criteria)
      2. **Project Scope** (what's included and explicitly excluded)
      3. **Timeline & Milestones** (key dates and deliverables)
      4. **Team Structure** (roles, responsibilities, stakeholders)
      5. **Resources & Budget** (if mentioned)
      6. **Dependencies & Risks Identified**
      7. **Communication Plan** (meetings, reporting, tools)
      8. **Immediate Next Steps**

      Format for easy reference as a project charter.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 2000

  - id: "openai-standup"
    name: "Daily Standup Summary"
    description: "Quick summary of daily standup with blockers and progress"
    type: "openai"
    prompt: |
      Summarize this standup meeting in a concise format:

      **Progress Updates:**
      - List what each person completed or is working on

      **Blockers:**
      - Highlight any impediments or issues raised

      **Today's Focus:**
      - Key priorities for the day

      **Help Needed:**
      - Any requests for assistance or collaboration

      Keep it brief and actionable - suitable for quick reference.
    model: "gpt-3.5-turbo"
    temperature: 0.2
    max_tokens: 800

  - id: "openai-company-webinar"
    name: "Company Webinar Summary"
    description: "Internal communication summary with key announcements and updates"
    type: "openai"
    prompt: |
      Summarize this internal company webinar/communication:

      1. **Major Announcements** (company news, changes, initiatives)
      2. **Strategic Direction** (company goals, vision, priorities)
      3. **Organizational Changes** (leadership, structure, process updates)
      4. **Key Metrics/Results Shared** (financial, growth, performance)
      5. **Employee Impact** (what this means for employees)
      6. **Q&A Highlights** (important questions and answers)
      7. **Important Dates & Deadlines**
      8. **Resources/Links Mentioned**

      Write in an informative tone suitable for sharing with those who missed the session.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1800

  - id: "openai-client-meeting"
    name: "Client Meeting Notes"
    description: "Client-focused summary with requirements and commitments"
    type: "openai"
    prompt: |
      Create professional client meeting notes covering:

      1. **Meeting Attendees & Purpose**
      2. **Client Requirements/Requests** (detailed list)
      3. **Solutions/Proposals Discussed**
      4. **Agreements & Commitments Made** (by both parties)
      5. **Timeline & Deliverables**
      6. **Budget/Pricing Discussion** (if applicable)
      7. **Client Concerns/Feedback**
      8. **Action Items** (clearly marked who does what)
      9. **Next Meeting/Follow-up Plans**

      Use professional client-facing language.
    model: "gpt-3.5-turbo"
    temperature: 0.2
    max_tokens: 1500

  - id: "openai-retrospective"
    name: "Sprint Retrospective"
    description: "Agile retrospective with what went well, what didn't, and improvements"
    type: "openai"
    prompt: |
      Structure this retrospective meeting using the standard format:

      **What Went Well ✅**
      - List positive outcomes and successes

      **What Didn't Go Well ❌**
      - Challenges, issues, or pain points encountered

      **Action Items for Improvement 🎯**
      - Specific, actionable improvements to implement
      - Assign owners if mentioned

      **Appreciations 🙏**
      - Team member shoutouts and recognition

      **Carry Forward**
      - Items to revisit in next retrospective

      Focus on constructive and actionable feedback.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1200

  - id: "openai-brainstorm"
    name: "Brainstorming Session"
    description: "Capture all ideas, evaluate them, and identify top candidates"
    type: "openai"
    prompt: |
      Organize this brainstorming session output:

      1. **All Ideas Generated** (comprehensive list with brief descriptions)
      2. **Ideas by Category** (group related concepts)
      3. **Top Ideas Selected** (most promising with rationale)
      4. **Ideas Requiring More Research**
      5. **Ideas to Prototype/Test**
      6. **Ideas Parked for Future**
      7. **Next Steps** (how to move forward with selected ideas)

      Preserve creative language while organizing for clarity.
    model: "gpt-3.5-turbo"
    temperature: 0.4
    max_tokens: 1800

  - id: "openai-training-session"
    name: "Training Session Notes"
    description: "Educational content summary with key learnings and resources"
    type: "openai"
    prompt: |
      Create comprehensive training session notes:

      1. **Topic/Subject Overview**
      2. **Key Concepts Covered** (main learning objectives)
      3. **Important Details & Examples**
      4. **Step-by-Step Processes** (if applicable)
      5. **Common Mistakes/Pitfalls to Avoid**
      6. **Best Practices Highlighted**
      7. **Resources/Documentation Links Mentioned**
      8. **Practice Exercises or Homework**
      9. **Questions Asked & Answers**
      10. **Follow-up/Additional Learning Needed**

      Format as study notes for future reference.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 2000

  - id: "openai-decision-record"
    name: "Decision Record (ADR Style)"
    description: "Architecture Decision Record format for important technical choices"
    type: "openai"
    prompt: |
      Create an Architecture Decision Record (ADR) from this discussion:

      **Status:** [Decision made/pending/superseded]

      **Context:**
      What is the issue we're trying to solve? What are the driving factors?

      **Decision:**
      What is the change that we're actually proposing/doing?

      **Consequences:**
      - Positive outcomes and benefits
      - Negative consequences and trade-offs
      - Risks and mitigations

      **Alternatives Considered:**
      What other options were discussed and why weren't they chosen?

      **Implementation Notes:**
      Any technical details about how this will be implemented.

      Use clear technical language suitable for documentation.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 1500

  - id: "openai-interview-notes"
    name: "Interview Summary"
    description: "Candidate interview notes with assessment and feedback"
    type: "openai"
    prompt: |
      Create structured interview notes:

      1. **Candidate Information** (name, role, date if mentioned)
      2. **Technical Skills Assessment**
         - Skills demonstrated
         - Technical question responses
         - Problem-solving approach
      3. **Soft Skills & Culture Fit**
         - Communication style
         - Team collaboration indicators
         - Cultural alignment
      4. **Strengths Identified**
      5. **Areas of Concern**
      6. **Notable Responses** (impressive answers or red flags)
      7. **Questions Candidate Asked** (shows their interests/priorities)
      8. **Overall Impression**
      9. **Recommendation** (if decision was discussed)
      10. **Next Steps**

      Maintain objectivity and professionalism.
    model: "gpt-3.5-turbo"
    temperature: 0.2
    max_tokens: 1500

  - id: "openai-incident-postmortem"
    name: "Incident Postmortem"
    description: "Document incident details, root cause, and prevention measures"
    type: "openai"
    prompt: |
      Create a comprehensive incident postmortem:

      **Incident Summary**
      - What happened and when
      - Impact (users affected, downtime, etc.)
      - Severity level

      **Timeline**
      - Detection time
      - Key events during incident
      - Resolution time

      **Root Cause Analysis**
      - What caused the incident
      - Contributing factors

      **Resolution Steps Taken**
      - Immediate fixes applied
      - How normal service was restored

      **What Went Well**
      - Effective responses and processes

      **What Could Be Improved**
      - Gaps in monitoring, process, or response

      **Action Items**
      - Preventive measures
      - Process improvements
      - Technical changes needed
      - Owners and deadlines

      **Lessons Learned**

      Use blameless postmortem approach.
    model: "gpt-4"
    temperature: 0.2
    max_tokens: 2000
`
}
