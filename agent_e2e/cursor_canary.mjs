import { Agent } from "@cursor/sdk";
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
    "You are running a Cursor SDK MCP canary against a disposable Scribe server.",
    "Follow these steps exactly and do not skip any required tool call.",
    "1. Call the Scribe admin tool to get the motd.",
    `2. Call the Scribe artifact tool to create a task with {"action":"create","kind":"task","title":"${title}","scope":"${scope}","priority":"${priority}"}.`,
    `3. Call the Scribe artifact tool to list artifacts in scope "${scope}" with {"action":"list","scope":"${scope}","fields":["id","title","kind"]}.`,
    '4. Reply with exactly one sentence that starts with "done:" and includes the created artifact ID.',
  ].join("\n");
}

async function main() {
  const apiKey = requireEnv("CURSOR_API_KEY");
  const workdir = requireEnv("CURSOR_CANARY_WORKDIR");
  const scribeURL = requireEnv("CURSOR_CANARY_SCRIBE_URL");
  const title = requireEnv("CURSOR_CANARY_TITLE");
  const scope = optionalEnv("CURSOR_CANARY_SCOPE", "cursor-e2e");
  const priority = optionalEnv("CURSOR_CANARY_PRIORITY", "medium");
  const modelId = optionalEnv("CURSOR_MODEL", "composer-2");

  await fs.mkdir(workdir, { recursive: true });
  await fs.writeFile(
    path.join(workdir, "README.md"),
    "# Cursor SDK canary workspace\n\nThis directory is disposable.\n",
    { flag: "w" },
  );

  const agent = await Agent.create({
    apiKey,
    model: { id: modelId },
    local: {
      cwd: workdir,
      settingSources: [],
    },
    mcpServers: {
      scribe: {
        type: "http",
        url: scribeURL,
      },
    },
  });

  const prompt = makePrompt(title, scope, priority);
  const observedCalls = [];

  try {
    const run = await agent.send(prompt);

    for await (const event of run.stream()) {
      if (event.type === "system") {
        console.log(
          JSON.stringify({
            event: "system",
            tools: Array.isArray(event.tools) ? event.tools : [],
          }),
        );
        continue;
      }
      if (event.type !== "tool_call") {
        continue;
      }
      const summary = {
        event: "tool_call",
        name: event.name,
        status: event.status,
        args: event.args ?? null,
      };
      observedCalls.push(summary);
      console.log(JSON.stringify(summary));
    }

    const result = await run.wait();
    if (result.status !== "finished") {
      throw new Error(`run ended with status ${result.status}: ${result.result ?? ""}`);
    }

    const sawMotd = observedCalls.some((call) =>
      hasNestedObject(call.args, (obj) => obj.action === "motd"),
    );
    const sawCreate = observedCalls.some((call) =>
      hasNestedObject(
        call.args,
        (obj) =>
          obj.action === "create" &&
          obj.kind === "task" &&
          obj.title === title &&
          obj.scope === scope,
      ),
    );
    const sawList = observedCalls.some((call) =>
      hasNestedObject(call.args, (obj) => obj.action === "list" && obj.scope === scope),
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
        finalResult: result.result ?? "",
      }),
    );
  } finally {
    await agent[Symbol.asyncDispose]();
  }
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  console.error(JSON.stringify({ ok: false, error: message }));
  process.exit(1);
});
