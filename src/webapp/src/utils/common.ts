/*
 * @Author: Jmeow
 * @Date: 2020-01-28 11:25:44
 * @Description: common utils
 */

function customFetch(arg1: Parameters<typeof fetch>[0], ...args: any[]) {
    return new Promise((resolve, reject) => {
        fetch.call(null, arg1, ...args)
            .then(async (rsp) => {
                if (rsp.ok) {
                    return rsp.json();
                } else {
                    // Try to parse error message from JSON or text
                    const clonedRsp = rsp.clone();
                    let errMsg = '';
                    try {
                        const data = await rsp.json();
                        errMsg = data.err_msg || data.message || rsp.statusText;
                    } catch (e) {
                        try {
                            errMsg = await clonedRsp.text() || rsp.statusText;
                        } catch (e2) {
                            errMsg = rsp.statusText;
                        }
                    }
                    throw new Error(errMsg);
                }
            })
            .then(data => {
                resolve(data);
            })
            .catch(err => {
                Utils.alertError(err);
                reject(err);
            });
    });
}

/**
 * 复制文本到剪贴板
 *
 * 兼容说明：页面在 http（非安全上下文）下没有 navigator.clipboard，
 * 直接调用会抛错，因此统一走本函数：优先 Clipboard API，失败时降级为
 * document.execCommand('copy')，保证 http 场景复制按钮仍可用。
 * @param text 待复制文本
 * @returns 是否复制成功
 */
export async function copyTextToClipboard(text: string): Promise<boolean> {
    if (!text) {
        return false;
    }
    // 优先使用 Clipboard API（仅安全上下文可用）
    if (
        typeof navigator !== 'undefined'
        && navigator.clipboard
        && typeof navigator.clipboard.writeText === 'function'
    ) {
        try {
            await navigator.clipboard.writeText(text);
            return true;
        } catch {
            // 失败后继续尝试降级方案
        }
    }
    return execCommandCopy(text);
}

/** 降级复制实现：临时 textarea + document.execCommand('copy')，在 http 下同样可用 */
function execCommandCopy(text: string): boolean {
    try {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.setAttribute('readonly', 'readonly');
        textarea.style.position = 'fixed';
        textarea.style.top = '0';
        textarea.style.left = '0';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        textarea.setSelectionRange(0, textarea.value.length);
        const copied = document.execCommand('copy');
        document.body.removeChild(textarea);
        return copied;
    } catch (error) {
        console.error('复制到剪贴板失败:', error);
        return false;
    }
}

class Utils {
    /**
     * Get request
     * @param url URL
     */
    requestGet(url: string) {
        return customFetch(url);
    }

    /**
     * Post request
     * @param url URL
     * @param body Request body
     */
    requestPost(url: string, body?: object) {
        return customFetch(url, {
            method: 'POST',
            body: JSON.stringify(body),
            headers: new Headers({
                'Content-Type': 'application/json'
            })
        });
    }

    /**
     * Post request
     * @param url URL
     * @param body Request body
     */
    requestPut(url: string, body?: object) {
        return customFetch(url, {
            method: 'PUT',
            body: JSON.stringify(body),
            headers: new Headers({
                'Content-Type': 'application/json'
            })
        })
    }

    /**
     * Delete request
     * @param url URL
     */
    requestDelete(url: string) {
        return customFetch(url, {
            method: 'DELETE'
        });
    }

    /**
     * Patch request
     * @param url URL
     * @param body Request body
     */
    requestPatch(url: string, body?: object) {
        return customFetch(url, {
            method: 'PATCH',
            body: JSON.stringify(body),
            headers: new Headers({
                'Content-Type': 'application/json'
            })
        });
    }

    /**
     * Show Error 
     * @param err error Object
     */
    static alertError(err?: any) {
        console.error(err ? err : "Server Error!");
    }

    static byteSizeToHumanReadableFileSize(size: number): string {
        if (!size) {
            return "0";
        }
        const i = Math.floor(Math.log(size) / Math.log(1024));
        const ret = Number((size / Math.pow(1024, i)).toFixed(2)) + " " + ['B', 'kB', 'MB', 'GB', 'TB'][i];
        return ret;
    }

    static timestampToHumanReadable(timestamp: number): string {
        const date = new Date(timestamp * 1000);
        const year = date.getFullYear().toString().padStart(4, "0");
        const month = (date.getMonth() + 1).toString().padStart(2, "0");
        const day = date.getDate().toString().padStart(2, "0");
        const hour = date.getHours().toString().padStart(2, "0");
        const min = date.getMinutes().toString().padStart(2, "0");
        const sec = date.getSeconds().toString().padStart(2, "0");
        return `${year}-${month}-${day} ${hour}:${min}:${sec}`;
    }
}

export default Utils;
