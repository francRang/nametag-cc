# Key Decisions

- Program will be written in Go, this is what I am familiar with and I know for a fact it supports multiple-architecture/OS's with `GOOS GOARCH` variables set when executing `go build`.
- Program supports crontab and interval pull updates
- Program supports GH Webook + Relay server that listens for events and client that updates the program
- I am using ngrok to forward my localhost to a public IP that handles TLS + deploys the relay server that the GH webhook publishes to.

# Questions

* What architectures should the runtime be built for?
- Abide by industry standards, that is: Linux: AMD64, Arm64. MacOS: AMD64 (Intel chips), ARM64. Windows: AMD64.

* On Windows, you cannot delete a running executable like in MacOS or Linux. How do you work around this? 
- You can always rename the running executable from name A to something different, create the new executable with name A, then clean-up the older executable (running, so stop, then delete it).

* What is considered production quality code?
- Go code that follows certain style standards (e.g: https://github.com/uber-go/guide/blob/master/style.md), 
- Code that has proper comments and a detailed README.md
- Code that has proper tests
- Code that has CI pipeline (program is distributed from binary, no need for CD here)
- Code that has a way to test things locally (Makefile in this case) as to not slow down the development process
- Code that uses concurrency when needed
- Code that is minimalistic in the number of external packages it uses to minimize potential future vulnerabilities
- If external packages are used, ensure that the package is actively maintained
- Code that has a diagram to illustrate the architecture and functionality of the program

* What should the program do?
- I think this should be minimalistic, as long as it prints the version change on update for PoC, that's fine.
* How should the program upgrade during execution?
- I think adding interval and crontab support makes sense if this was a daemon program. It also makes things easier depending on the environment where it lives (egress/ingress restrictions, firewall).
- It should also have a webhook functionality that allows the program to get a release as soon as it published without relying on timing.
