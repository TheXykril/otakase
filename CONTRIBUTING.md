# Contributing

Pull requests come from the project's collaborators only. Pull requests from
outside the team are closed without review, so please don't spend time on one.

You can still help:

- **Bugs:** open an issue with the steps to reproduce it and the end of
  `otakase -diag`.
- **Ideas and questions:** ask on [Discord](https://otakase.xyverion.com/discord/).
- **Skip times:** mark and vote on openings and endings from the player
  (`ContributeSkipTimes`).

## For collaborators

Open pull requests against `main` with a conventional title (`fix(scope): …`,
`feat(scope): …`, `chore: …`) and fill in the template. CI (`go vet`,
`go test -short -race ./...` and a Linux build) runs only when started by
hand, so run the checks locally before asking for a merge.

Version numbering and how a release is cut are on the wiki's
[Versions and releases](https://github.com/TheXykril/otakase/wiki/Developer-Releases)
page. More on building, testing and internals is in the
[Developer section](https://github.com/TheXykril/otakase/wiki/Developer).
