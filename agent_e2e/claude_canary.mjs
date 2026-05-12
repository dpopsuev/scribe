import { query } from "@anthropic-ai/claude-agent-sdk";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";

function requireEnv(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`missing required environment variable ${name}`);
  }
  return value;
}

function optionalEnv(name, fallback) {
  const value = process.env[name];
  return value && value.length > 0 ? value : fallback;
}

function collectObjects(value, out = []) {
  if (!value || typeof value !== "object") {
    return out;
  }
  out.push(value);
  if (Array.isArray(value)) {
    for (const item of value) {
      collectObjects(item, out);
    }
    return out;
  }
  for (const child of Object.values(value)) {
    collectObjects(child, out);
  }
  return out;
}

function hasNestedObject(value, predicate) {
  return collectObjects(value).some((obj) => predicate(obj));
}

function makePrompt(title, scope, priority) {
  return [
    "You are running a Claude Agent SDK MCP canary against a disposable Scribe server.",
    "Follow these steps exactly and do not skip any required tool call.",
    "1. Call the Scribe admin tool to get the motd.",
    `2. Call the Scribe artifact tool to create a task with {"action":"create","kind":"task","title":"${title}","scope":"${scope}","priority":"${priority}"}.`,
    `3. Call the Scribe artifact tool to list artifacts in scope "${scope}" with {"action":"list","scope":"${scope}","fields":["id","title","kind"]}.`,
    '4. Reply with exactly one sentence that starts with "done:" and includes the created artifact ID.',
  ].join("\n");
}

async function main() {
  requireEnv("ANTHROPIC_API_KEY");

  const workdir = requireEnv("CLAUDE_CANARY_WORKDIR");
  const scribeURL = requireEnv("CLAUDE_CANARY_SCRIBE_URL");
  const title = requireEnv("CLAUDE_CANARY_TITLE");
  const scope = optionalEnv("CLAUDE_CANARY_SCOPE", "claude-e2e");
  const priority = optionalEnv("CLAUDE_CANARY_PRIORITY", "medium");
  const model = process.env.CLAUDE_MODEL;

  await fs.mkdir(workdir, { recursive: true });
  await fs.writeFile(
    path.join(workdir, "README.md"),
    "# Claude Agent SDK canary workspace\n\nThis directory is disposable.\n",
    { flag: "w" },
  );

  const observedCalls = [];
  const prompt = makePrompt(title, scope, priority);

  const q = query({
    prompt,
    options: {
      ...(model ? { model } : {}),
      cwd: workdir,
      settingSources: [],
      persistSession: false,
      maxTurns: 8,
      permissionMode: "dontAsk",
      allowedTools: ["mcp__scribe__admin", "mcp__scribe__artifact", "mcp__scribe__graph"],
      strictMcpConfig: true,
      mcpServers: {
        scribe: {
          type: "http",
          url: scribeURL,
          alwaysLoad: true,
        },
      },
      hooks: {
        PreToolUse: [
          {
            matcher: "mcp__scribe__.*",
            hooks: [
              async (input) => {
                const summary = {
                  event: "pre_tool_use",
                  tool_name: input.tool_name,
                  tool_input: input.tool_input,
                };
                observedCalls.push(summary);
                console.log(JSON.stringify(summary));
                return { continue: true };
              },
            ],
          },
        ],
      },
    },
  });

  let finalResult = "";

  try {
    for await (const message of q) {
      if (message.type === "system" && message.subtype === "init") {
        console.log(
          JSON.stringify({
            event: "system",
            model: message.model,
            mcp_servers: message.mcp_servers,
            tools: message.tools,
          }),
        );
        continue;
      }
      if (message.type === "result") {
        if (message.is_error) {
          const errors = "errors" in message ? message.errors.join("; ") : "";
          throw new Error(`query ended with error: ${errors}`);
        }
        finalResult = message.result ?? "";
        console.log(
          JSON.stringify({
            event: "result",
            subtype: message.subtype,
            result: finalResult,
          }),
        );
      }
    }

    const sawMotd = observedCalls.some((call) =>
      call.tool_name.endsWith("__admin") &&
      hasNestedObject(call.tool_input, (obj) => obj.action === "motd"),
    );
    const sawCreate = observedCalls.some((call) =>
      call.tool_name.endsWith("__artifact") &&
      hasNestedObject(
        call.tool_input,
        (obj) =>
          obj.action === "create" &&
          obj.kind === "task" &&
          obj.title === title &&
          obj.scope === scope,
      ),
    );
    const sawList = observedCalls.some((call) =>
      call.tool_name.endsWith("__artifact") &&
      hasNestedObject(call.tool_input, (obj) => obj.action === "list" && obj.scope === scope),
    );

    if (!sawMotd || !sawCreate || !sawList) {
      throw new Error(
        `missing expected Scribe tool calls (motd=${sawMotd}, create=${sawCreate}, list=${sawList})`,
      );
    }

    console.log(
      JSON.stringify({
        ok: true,
        observedToolCalls: observedCalls.length,
        finalResult,
      }),
    );
  } finally {
    q.close();
  }
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  console.error(JSON.stringify({ ok: false, error: message }));
  process.exit(1);
});
