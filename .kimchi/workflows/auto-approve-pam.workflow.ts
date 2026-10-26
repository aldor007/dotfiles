/**
 * auto-approve-pam — configure a requester allowlist and launch the PAM
 * auto-approver as a detached background watcher.
 *
 * Approves pending GCP PAM grant requests from the configured user(s) for
 * ANY entitlement (no --entitlement filter is passed to pam-approve.sh).
 *
 * Flow:
 *   1. questionnaire step: collect requester email(s), poll interval, the
 *      host (herdr default, tmux fallback) and the watcher name.
 *   2. function step: pre-flight checks, then launch
 *      `pam-approve.sh --auto --yes --users <requester> --interval <n>`
 *      detached in the chosen host with output visible there and teed to a
 *      log file, wait for the watch-mode banner, and report status plus the
 *      follow-up commands.
 *
 * Hosts:
 *   - herdr (default): split a small pane off the first existing pane, label
 *     it `pam-approve-auto`, and run the watcher inside it (output visible in
 *     herdr, teed to the log). Stop: close the pane or Ctrl-C inside it.
 *   - tmux: detached session `<name>` with the same tee'd log. Stop: attach
 *     and Ctrl-C, or kill-session.
 *
 * Safety notes:
 *   - `--yes` is passed because the questionnaire step itself is the explicit
 *     configuration act; the interactive typed-yes gate cannot be answered
 *     from a detached watcher.
 *   - Requester input is validated to a strict email-ish charset before being
 *     embedded in the shell command (no quotes/space/metacharacters).
 */
import { spawnSync } from "node:child_process"
import { existsSync, mkdirSync, readFileSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { Type, type Static } from "typebox"
import { createQuestionnaireStep, createStep, createWorkflow } from "@kimchi-dev/kimchi-workflows"

const SCRIPT_PATH = fileURLToPath(new URL("../../scripts/pam-approve.sh", import.meta.url))
const DOTFILES_ROOT = fileURLToPath(new URL("../..", import.meta.url))
const LOG_DIR = `${process.env.HOME ?? ""}/.local/state/pam-approve`
const BANNER = "AUTO-APPROVAL watch mode"
const REQUESTER_RE = /^[A-Za-z0-9._%+\-@]+(,[A-Za-z0-9._%+\-@]+)*$/
const NAME_RE = /^[A-Za-z0-9_-]{1,32}$/

const ConfigSchema = Type.Object({
	requester: Type.String({
		title: "Requester email(s)",
		description:
			"Only PAM grants requested by this user are auto-approved. Comma-separate for a list, e.g. alice@cast.ai,bob@cast.ai",
	}),
	intervalSeconds: Type.Integer({
		title: "Poll interval (seconds)",
		description: "How often the watcher re-scans for pending grants.",
		minimum: 5,
		default: 30,
	}),
	host: Type.Union([Type.Literal("herdr"), Type.Literal("tmux")], {
		title: "Host",
		description: "Where the detached watcher runs. herdr preferred (visible pane in your herdr workspace); tmux as fallback.",
		default: "herdr",
	}),
	name: Type.String({
		title: "Watcher name",
		description: "herdr pane label / tmux session name for the watcher.",
		default: "pam-approve-auto",
	}),
})

const ResultSchema = Type.Object({
	status: Type.Union([Type.Literal("running"), Type.Literal("failed")]),
	host: Type.String(),
	target: Type.String(),
	logPath: Type.String(),
	detail: Type.String(),
})

const configure = createQuestionnaireStep({
	name: "configure-auto-approve",
	description: "Collect the requester allowlist and watcher settings",
	output: ConfigSchema,
})

function fail(host: string, target: string, logPath: string, detail: string): Static<typeof ResultSchema> {
	return { status: "failed", host, target, logPath, detail }
}

const launch = createStep({
	name: "launch-watcher",
	description: "Pre-flight, launch the watcher detached (herdr pane or tmux session), wait for the watch-mode banner",
	input: ConfigSchema,
	output: ResultSchema,
	maxDurationMs: 180_000,
	run: ({ input }): Static<typeof ResultSchema> => {
		const { requester, intervalSeconds, host, name } = input
		const logPath = `${LOG_DIR}/${name}.log`

		if (!existsSync(SCRIPT_PATH)) {
			return fail(host, "", logPath, `script not found: ${SCRIPT_PATH}`)
		}
		if (!REQUESTER_RE.test(requester)) {
			return fail(host, name, logPath, `invalid requester value '${requester}' — use comma-separated emails (letters, digits, ._%+-@ only)`)
		}
		if (!NAME_RE.test(name)) {
			return fail(host, name, logPath, `invalid watcher name '${name}' (letters, digits, _ - only)`)
		}

		// Command typed into the host's shell: visible in the pane/session AND
		// teed to the log file used for verification.
		const cmdline = `'${SCRIPT_PATH}' --auto --yes --users '${requester}' --interval ${intervalSeconds} 2>&1 | tee '${logPath}'`

		mkdirSync(LOG_DIR, { recursive: true })
		return host === "herdr"
			? launchHerdr(name, requester, intervalSeconds, logPath, cmdline)
			: launchTmux(name, requester, intervalSeconds, logPath, cmdline)
	},
})

function launchHerdr(
	name: string,
	requester: string,
	intervalSeconds: number,
	logPath: string,
	cmdline: string,
): Static<typeof ResultSchema> {
	const run = (cmd: string, args: string[]) => spawnSync(cmd, args, { encoding: "utf8" })

	const status = run("herdr", ["status"])
	if (status.error || !`${status.stdout}${status.stderr}`.includes("status: running")) {
		return fail("herdr", name, logPath, "herdr server not running — start it with `herdr`, or choose the tmux host instead")
	}

	const listRaw = run("herdr", ["pane", "list"])
	if (listRaw.error || listRaw.status !== 0) {
		return fail("herdr", name, logPath, `herdr pane list failed: ${listRaw.stderr ?? listRaw.error?.message ?? "unknown error"}`)
	}
	let panes: Array<Record<string, unknown>> = []
	try {
		panes = (JSON.parse(listRaw.stdout) as { result?: { panes?: Array<Record<string, unknown>> } }).result?.panes ?? []
	} catch (e) {
		return fail("herdr", name, logPath, `could not parse herdr pane list output: ${String(e)}`)
	}

	const existing = panes.find(
		(p) => p.label === name || String(p.terminal_title_stripped ?? "").includes(name),
	)
	if (existing) {
		return fail(
			"herdr",
			String(existing.pane_id ?? name),
			logPath,
			`a pane labeled '${name}' already exists (${existing.pane_id}) — stop it first: herdr pane close ${existing.pane_id}`,
		)
	}
	const anchorPane = panes[0]
	if (!anchorPane) {
		return fail("herdr", name, logPath, "no herdr panes found to split from — open herdr and create a pane, or choose the tmux host")
	}

	const anchor = String(anchorPane.pane_id)
	const split = run("herdr", [
		"pane", "split", anchor,
		"--direction", "down", "--ratio", "0.2", "--no-focus",
		"--cwd", DOTFILES_ROOT,
	])
	if (split.error || split.status !== 0) {
		return fail("herdr", anchor, logPath, `herdr pane split failed: ${split.stderr ?? split.error?.message ?? "unknown error"}`)
	}
	let paneId = ""
	try {
		paneId = String((JSON.parse(split.stdout) as { result?: { pane?: { pane_id?: string } } }).result?.pane?.pane_id ?? "")
	} catch (e) {
		return fail("herdr", anchor, logPath, `could not parse herdr pane split output: ${String(e)}`)
	}
	if (!paneId) {
		return fail("herdr", anchor, logPath, "herdr pane split returned no pane_id")
	}

	run("herdr", ["pane", "rename", paneId, name]) // cosmetic; ignore failures

	const started = run("herdr", ["pane", "run", paneId, cmdline])
	if (started.error || started.status !== 0) {
		return fail("herdr", paneId, logPath, `herdr pane run failed: ${started.stderr ?? started.error?.message ?? "unknown error"}`)
	}

	const waited = waitForBanner(logPath)
	if (waited) {
		return {
			status: "running",
			host: "herdr",
			target: paneId,
			logPath,
			detail:
				`auto-approving grants requested by '${requester}' for ANY entitlement (interval ${intervalSeconds}s) — ` +
				`herdr pane ${paneId} (labeled '${name}'). Stop: herdr pane close ${paneId} (or focus the pane and press Ctrl-C) · Log: tail -f ${logPath}`,
		}
	}
	return fail(
		"herdr",
		paneId,
		logPath,
		`started but no '${BANNER}' banner within 15s — inspect the herdr pane ${paneId}, the log (${logPath}) or stop it: herdr pane close ${paneId}`,
	)
}

function launchTmux(
	name: string,
	requester: string,
	intervalSeconds: number,
	logPath: string,
	cmdline: string,
): Static<typeof ResultSchema> {
	const run = (cmd: string, args: string[]) => spawnSync(cmd, args, { encoding: "utf8" })

	const tmuxCheck = run("tmux", ["-V"])
	if (tmuxCheck.error || tmuxCheck.status !== 0) {
		return fail("tmux", name, logPath, "tmux not available on PATH")
	}
	const exists = run("tmux", ["has-session", "-t", name])
	if (exists.status === 0) {
		return fail(
			"tmux",
			name,
			logPath,
			`tmux session '${name}' already exists — attach with: tmux attach -t ${name} (Ctrl-C stops cleanly) or kill it first: tmux kill-session -t ${name}`,
		)
	}
	const started = run("tmux", ["new-session", "-d", "-s", name, cmdline])
	if (started.error || started.status !== 0) {
		return fail("tmux", name, logPath, `tmux new-session failed: ${started.stderr ?? started.error?.message ?? "unknown error"}`)
	}

	const waited = waitForBanner(logPath)
	if (waited) {
		return {
			status: "running",
			host: "tmux",
			target: name,
			logPath,
			detail:
				`auto-approving grants requested by '${requester}' for ANY entitlement (interval ${intervalSeconds}s) — ` +
				`tmux session '${name}'. Watch: tmux attach -t ${name} (Ctrl-C stops cleanly) · Stop: tmux kill-session -t ${name} · Log: tail -f ${logPath}`,
		}
	}
	return fail(
		"tmux",
		name,
		logPath,
		`started but no '${BANNER}' banner within 15s — session may still be initializing; check ${logPath} and tmux attach -t ${name}`,
	)
}

/** Poll the tee'd log for the watcher banner (max ~15s). */
function waitForBanner(logPath: string): boolean {
	for (let i = 0; i < 15; i += 1) {
		try {
			if (readFileSync(logPath, "utf8").includes(BANNER)) return true
		} catch {
			// log file may not exist yet on the first iterations
		}
		Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 1000)
	}
	return false
}

export default createWorkflow({
	name: "auto-approve-pam",
	description:
		"Configure a requester allowlist and launch the PAM auto-approver detached (herdr pane by default, tmux fallback) — approves that user's pending grants for ANY entitlement",
})
	.then(configure)
	.then(launch)
	.commit()
