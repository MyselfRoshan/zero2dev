// Pure helpers for the "Command line" section of Settings: where the shims folder is and
// the lines a user types to activate Zero2Dev's tools in the current terminal.
import { joinPath } from "./installer";

/** <data folder>\shims (envreg.ShimsDir). */
export function shimsDir(homeDir: string): string {
  return joinPath(homeDir, "shims");
}

/** The line to run in cmd.exe. `call` keeps it working from inside other batch files. */
export function cmdActivateLine(homeDir: string): string {
  return `call "${joinPath(shimsDir(homeDir), "activate.cmd")}"`;
}

/** The line to run in PowerShell: dot-sourced so PATH changes stay in the session. */
export function psActivateLine(homeDir: string): string {
  return `. '${joinPath(shimsDir(homeDir), "activate.ps1").replace(/['‘’‚‛]/g, (q) => q + q)}'`;
}
