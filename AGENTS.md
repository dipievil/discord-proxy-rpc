# PROJECT OVERVIEW

This is Rich Presence agent that send activities to a running Discord on your LAN.

## General behaviour

- For any decision that is not clear, ask user for clarification
- Always suggest best options to user based on security, performance and maintainability
- If any task resolution is different than the initial spec, update the spec files on `docs/implementation` folder and add a comment to it's card
- Implementation files are on `docs/implementation` folder. If user ask for different approach than specified, update these files.
- If user provide any new information about the project, not related to a specific task, add to history
- NEVER commit sensitive information to the repository. Warn user for any sensitive information that is about to be commited and pushed to remote repository

## Development rules

- Follow best practices on development
- Prefer SOLID principles and clean code
- Avoid code comments, unless necessary for clarification and functional documentation
- Use Context7 MCP for updated technology information
- Immediately test local changes on a development environment before pushing to production

## Tasks management

- Initial spec files are on folder `docs/implementation`. Follow it. If user ask for different approach update these files.
- Use Trello board `app-game-rpc`(6a81cb6c9ae0484b113e28bf) to track current and new stories
- Stories are ordered by phases and tasks (1.1, 1.2, etc.). Should be completed in order, unless user ask for different approach
  - New stories with no blockers and dependencies are on `backlog` list
  - Tasks ready to start are on `To-do` list
  - On going tasks are on `Doing`
  - Tasks that cant be finished are on `Blocked` list
  - Completed tasks should be on `Done` list
  - Tasks that are no needed anymore are on `Cancelled` list
- Only start to work on tasks on `To-do` or `Doing` list
- If a story cannot be finished, add a comment to it's card than move to `Blocked`
- Before start any task:
  - Check if ther is any card on Trello `To-do` list
  - If empty STOP IMMEDIATELLY and ask user what to do:
    - Create a card directly on `Doing` with more details; or
    - Continue working with no tracking
  - If no card is avaiable, move the top 5 cards from `backlog` to `to-do` list
  - For different approaches than specified on the story, add a comment to it on Trello
- Once a story is finished:
  - move its card to `Done`
  - check if a blocked story can be unblocked. If so, move the card on `Blocked` to `To-do`

## Code versioning

- Use conventional commits message format
- Use semantic versioning for releases
- Always ask user if a finished task should be commited and pushed to remote yrepositor
