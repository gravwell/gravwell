# Contributing to Gravwell's Open Source Repo

We welcome contributions in the form of issues and Github pull requests. This file documents some guidelines to keep in mind when contributing to the Gravwell repo.

## Content

Good issues should contain a detailed description of your problem and either steps to reproduce it or a minimal reproducer program.

Good pull requests should address problems which may be applicable to others, not just your specific use-case.

Please don't submit PRs which only make small tweaks to comments or documentation; stick to substantive changes, either modifications to code or repository-wide comment/documentation fixes.

Small changes should target the `next-patch` branch; more extensive modifications should probably target `next-minor`.

## Style

Please follow standard [Go style](https://go.dev/doc/effective_go), and always run `go fmt` before comitting your code.

## Dependencies

Never vendor dependencies; reference them via `go.mod` only. Never copy in portions of code wholesale from another package.

Any dependency must be [permissively licensed](https://en.wikipedia.org/wiki/Permissive_software_license), which is luckily the most common type of license in the Go world. The following licenses are known to be acceptable:

* Apache
* BSD
* ISC
* MIT

Try to avoid introducing too many new dependencies, as it introduces additional work for the Gravwell developers to track updates and deal with vulnerabilities.

## Testing

Please add tests for new functionality you add to packages in the repo.

You can run the full set of checks manually by running the following command at the top level:

```
bash .github/workflows/run_local_build_checksg.sh
```

Don't submit PRs that modify existing tests without discussing it with Gravwell first.
