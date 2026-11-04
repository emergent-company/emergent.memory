// Google Drive REST fixture for the opt-in `gdrive-live` project. It drives a
// real Drive account through a fixture refresh token (full `drive` scope,
// produced by scripts/gdrive-fixture-token.mjs) so the spec can create, edit,
// and trash the exact files the live source sync must observe.
//
// Credential hygiene: the refresh token is held in memory only and the short-
// lived access token is cached and refreshed silently. No token value is ever
// returned, logged, or attached — callers only ever see file ids/revisions.

const DRIVE_API = 'https://www.googleapis.com/drive/v3';
const UPLOAD_API = 'https://www.googleapis.com/upload/drive/v3';
const TOKEN_URL = 'https://oauth2.googleapis.com/token';

/** The subset of a Drive file resource the fixture reads back. */
export interface DriveFile {
  id: string;
  name?: string;
  headRevisionId?: string;
  trashed?: boolean;
}

interface TokenResponse {
  access_token: string;
  expires_in?: number;
  token_type?: string;
}

export class GDriveFixture {
  private accessToken = '';
  private expiresAt = 0;

  constructor(
    private readonly clientId: string,
    private readonly clientSecret: string,
    private readonly refreshToken: string,
  ) {}

  /**
   * Exchange the refresh token for a short-lived access token, cached in memory
   * until just before expiry. Returns the token value but never exposes it.
   */
  private async tokenValue(): Promise<string> {
    if (this.accessToken && Date.now() < this.expiresAt - 60_000) {
      return this.accessToken;
    }
    const body = new URLSearchParams({
      grant_type: 'refresh_token',
      client_id: this.clientId,
      client_secret: this.clientSecret,
      refresh_token: this.refreshToken,
    });
    const resp = await fetch(TOKEN_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
    if (!resp.ok) {
      throw new Error(`gdrive token refresh failed (HTTP ${resp.status})`);
    }
    const payload = (await resp.json()) as TokenResponse;
    if (!payload.access_token) {
      throw new Error('gdrive token refresh returned no access_token');
    }
    this.accessToken = payload.access_token;
    this.expiresAt = Date.now() + (payload.expires_in ?? 3600) * 1000;
    return this.accessToken;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    return { Authorization: `Bearer ${await this.tokenValue()}` };
  }

  private async jsonHeaders(): Promise<Record<string, string>> {
    return { ...(await this.authHeaders()), 'Content-Type': 'application/json' };
  }

  /** Create a folder (or any Drive file) from metadata JSON. */
  private async createFileMeta(meta: Record<string, unknown>): Promise<DriveFile> {
    const resp = await fetch(`${DRIVE_API}/files`, {
      method: 'POST',
      headers: await this.jsonHeaders(),
      body: JSON.stringify(meta),
    });
    if (!resp.ok) {
      throw new Error(`gdrive create failed (HTTP ${resp.status}): ${await this.textOr(resp)}`);
    }
    return (await resp.json()) as DriveFile;
  }

  private async textOr(resp: Response): Promise<string> {
    try {
      return (await resp.text()).slice(0, 500);
    } catch {
      return '';
    }
  }

  /** Create an empty folder; returns its id. */
  async createFolder(name: string): Promise<string> {
    const folder = await this.createFileMeta({
      name,
      mimeType: 'application/vnd.google-apps.folder',
    });
    return folder.id;
  }

  /**
   * Create a plain-text file under `folderId` and write `content`. Uses the
   * metadata-create + media-upload pair (avoids multipart/related), then reads
   * the file back so the returned `headRevisionId` is authoritative.
   */
  async uploadTextFile(folderId: string, name: string, content: string): Promise<DriveFile> {
    const created = await this.createFileMeta({
      name,
      mimeType: 'text/plain',
      parents: [folderId],
    });
    await this.writeMedia(created.id, content);
    return this.getFile(created.id);
  }

  /** Overwrite a file's content (media upload) and read back the new revision. */
  async updateFileContent(fileId: string, content: string): Promise<DriveFile> {
    await this.writeMedia(fileId, content);
    return this.getFile(fileId);
  }

  private async writeMedia(fileId: string, content: string): Promise<void> {
    const resp = await fetch(
      `${UPLOAD_API}/files/${encodeURIComponent(fileId)}?uploadType=media`,
      {
        method: 'PATCH',
        headers: { ...(await this.authHeaders()), 'Content-Type': 'text/plain' },
        body: content,
      },
    );
    if (!resp.ok) {
      throw new Error(`gdrive media upload failed (HTTP ${resp.status}): ${await this.textOr(resp)}`);
    }
  }

  /** Read a file's current id/name/headRevisionId/trashed. */
  async getFile(fileId: string): Promise<DriveFile> {
    const resp = await fetch(
      `${DRIVE_API}/files/${encodeURIComponent(fileId)}?fields=id,name,headRevisionId,trashed`,
      { headers: await this.authHeaders() },
    );
    if (!resp.ok) {
      throw new Error(`gdrive getFile failed (HTTP ${resp.status}): ${await this.textOr(resp)}`);
    }
    return (await resp.json()) as DriveFile;
  }

  /** Move a file to the trash (does not delete it outright). */
  async trashFile(fileId: string): Promise<void> {
    const resp = await fetch(`${DRIVE_API}/files/${encodeURIComponent(fileId)}`, {
      method: 'PATCH',
      headers: await this.jsonHeaders(),
      body: JSON.stringify({ trashed: true }),
    });
    if (!resp.ok) {
      throw new Error(`gdrive trash failed (HTTP ${resp.status}): ${await this.textOr(resp)}`);
    }
  }

  /** Permanently delete a file (bypasses the trash). Never throws. */
  async deleteFile(fileId: string): Promise<void> {
    try {
      await fetch(`${DRIVE_API}/files/${encodeURIComponent(fileId)}`, {
        method: 'DELETE',
        headers: await this.authHeaders(),
      });
    } catch {
      // Best-effort cleanup only.
    }
  }

  /**
   * Best-effort folder delete. A folder with children cannot be removed, so
   * callers must delete the fixture file (deleteFile) before this. Never
   * throws.
   */
  async deleteFolder(folderId: string): Promise<void> {
    try {
      await fetch(`${DRIVE_API}/files/${encodeURIComponent(folderId)}`, {
        method: 'DELETE',
        headers: await this.authHeaders(),
      });
    } catch {
      // Best-effort cleanup only.
    }
  }
}
