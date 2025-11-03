# HTTP Shell

This Go web app receives an HTTP POST request on `$PORT` that is encoded `application/x-www-form-urlencoded` and executes the command in the `text` field synchronously.

## Request Format

The server expects a POST request with form-encoded data containing at minimum a `text` field:

```
# application/x-www-form-urlencoded
text=$ date
```

Additional fields may be included but are not required (e.g., `channel_id`, `user_id`, `team_id`, etc. for compatibility with Slack webhook formats).

The leading `$` in the `text` field is automatically stripped before execution.

## Response

The command is executed synchronously in the shell, and the result is returned as a JSON response with `response_type: "in_channel"` and the command output in the `text` field. The response includes:

- Command output (stdout)
- Error output (stderr) if present
- Exit code
- Execution time

Example response:
```json
{
    "response_type": "in_channel",
    "text": "```\noutput from command\n```\n\nexit: 0 | 1.63ms\n"
}
```

## Configuration

- `PORT`: Server port (defaults to `8080`)

## Usage

Start the server:
```bash
export PORT=8080  # optional, defaults to 8080
go run main.go
```

Send a request:
```bash
curl -X POST http://localhost:8080 \
  -d "text=\$ echo hello world"
```

The server will execute the command and return the result in the response body.
