#!/usr/bin/env node
// Supervisor only. protocol-login.mjs is an external one-shot CLI and is not
// imported here. Each job still starts that CLI once. This process stays up so
// the worker does not cold-start a new Node supervisor between attempts.
import { spawn } from "node:child_process";
import readline from "node:readline";

const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });

input.on("line", (line) => {
  let job;
  try {
    job = JSON.parse(line);
  } catch {
    process.stdout.write(JSON.stringify({ returncode: 1, stdout: "", stderr: "invalid job" }) + "\n");
    return;
  }
  const command = Array.isArray(job.command) ? job.command.map(String) : [];
  if (command.length === 0) {
    process.stdout.write(JSON.stringify({ returncode: 1, stdout: "", stderr: "missing command" }) + "\n");
    return;
  }
  const child = spawn(command[0], command.slice(1), {
    cwd: typeof job.cwd === "string" ? job.cwd : undefined,
    env: job.env && typeof job.env === "object" ? job.env : process.env,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  let stdout = "";
  let stderr = "";
  let replied = false;
  const cap = 1_048_576;
  child.stdout.on("data", (chunk) => {
    if (stdout.length < cap) stdout += chunk.toString("utf8");
  });
  child.stderr.on("data", (chunk) => {
    if (stderr.length < cap) stderr += chunk.toString("utf8");
  });
  const timer = setTimeout(() => {
    child.kill();
  }, Math.max(1, Number(job.timeout) || 1) * 1000);
  const reply = (code) => {
    if (replied) return;
    replied = true;
    clearTimeout(timer);
    process.stdout.write(JSON.stringify({ returncode: code ?? 1, stdout, stderr }) + "\n");
  };
  child.on("close", reply);
  child.on("error", () => reply(1));
});
