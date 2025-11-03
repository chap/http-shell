# HTTP Shell

This go web app receives a Slack HTTP POST request on $PORT that is encoded `application/x-www-form-urlencoded` and executes the command in the `text` field.

```
# application/x-www-form-urlencoded
token=gIkuvaNzQIHg97ATvDxqgjtO
&team_id=T0001
&team_domain=example
&enterprise_id=E0001
&enterprise_name=Globular%20Construct%20Inc
&channel_id=C2147483705
&channel_name=test
&user_id=U2147483697
&user_name=Steve
&command=/h
&text=$+date
&response_url=https://hooks.slack.com/commands/1234/5678
&trigger_id=13345224609.738474920.8088930838d88f008e0
&api_app_id=A123456
```

When the request is received, a succssful response with an empty body is returned immediately.

Then a goroutine is spawned to start the chat stream:
https://docs.slack.dev/reference/methods/chat.startStream/

Another goroutine is spawned to execute the shell command in the `text` field. (Striping the leading '$'.)

Every 1 second, shell logs are appended to chat stream:
https://docs.slack.dev/reference/methods/chat.appendStream

When the shell process exits, basic debugging information (exit code, clock time, etc) is appended and the chat stream is stopped:
https://docs.slack.dev/reference/methods/chat.stopStream/

If SLACK_TOKEN is not set, the app fails to start.
