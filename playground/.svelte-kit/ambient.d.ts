
// this file is generated — do not edit it


/// <reference types="@sveltejs/kit" />

/**
 * This module provides access to environment variables that are injected _statically_ into your bundle at build time and are limited to _private_ access.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Static environment variables are [loaded by Vite](https://vitejs.dev/guide/env-and-mode.html#env-files) from `.env` files and `process.env` at build time and then statically injected into your bundle at build time, enabling optimisations like dead code elimination.
 * 
 * **_Private_ access:**
 * 
 * - This module cannot be imported into client-side code
 * - This module only includes variables that _do not_ begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) _and do_ start with [`config.kit.env.privatePrefix`](https://svelte.dev/docs/kit/configuration#env) (if configured)
 * 
 * For example, given the following build time environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { ENVIRONMENT, PUBLIC_BASE_URL } from '$env/static/private';
 * 
 * console.log(ENVIRONMENT); // => "production"
 * console.log(PUBLIC_BASE_URL); // => throws error during build
 * ```
 * 
 * The above values will be the same _even if_ different values for `ENVIRONMENT` or `PUBLIC_BASE_URL` are set at runtime, as they are statically replaced in your code with their build time values.
 */
declare module '$env/static/private' {
	export const SVELTEKIT_FORK: string;
	export const NODE_ENV: string;
	export const EDITOR: string;
	export const INIT_CWD: string;
	export const npm_config_global_prefix: string;
	export const QT_IM_MODULES: string;
	export const XDG_SESSION_EXTRA_DEVICE_ACCESS: string;
	export const XDG_DATA_DIRS: string;
	export const NVM_CD_FLAGS: string;
	export const npm_config_globalconfig: string;
	export const ANTIGRAVITY_TRAJECTORY_ID: string;
	export const VSCODE_CODE_CACHE_PATH: string;
	export const QT_IM_MODULE: string;
	export const GJS_DEBUG_OUTPUT: string;
	export const npm_config_init_module: string;
	export const QT_ACCESSIBILITY: string;
	export const npm_lifecycle_event: string;
	export const npm_package_version: string;
	export const GOPATH: string;
	export const ANTIGRAVITY_LS_ADDRESS: string;
	export const SSH_AUTH_SOCK: string;
	export const npm_lifecycle_script: string;
	export const SDKMAN_DIR: string;
	export const LS_COLORS: string;
	export const XDG_SESSION_DESKTOP: string;
	export const LANG: string;
	export const PYENV_ROOT: string;
	export const XDG_RUNTIME_DIR: string;
	export const GNOME_SETUP_DISPLAY: string;
	export const GIO_LAUNCHED_DESKTOP_FILE: string;
	export const XMODIFIERS: string;
	export const NVM_BIN: string;
	export const NVM_INC: string;
	export const npm_execpath: string;
	export const PYENV_SHELL: string;
	export const npm_command: string;
	export const GPG_AGENT_INFO: string;
	export const LOGNAME: string;
	export const ANTIGRAVITY_SOURCE_METADATA: string;
	export const ZSH: string;
	export const PATH: string;
	export const VSCODE_NLS_CONFIG: string;
	export const SYSTEMD_EXEC_PID: string;
	export const GDK_BACKEND: string;
	export const NVM_DIR: string;
	export const PAGER: string;
	export const DESKTOP_SESSION: string;
	export const BUN_INSTALL: string;
	export const XAUTHORITY: string;
	export const npm_config_local_prefix: string;
	export const CHROME_DESKTOP: string;
	export const ANTIGRAVITY_EDITOR_APP_ROOT: string;
	export const HOME: string;
	export const PWD: string;
	export const GJS_DEBUG_TOPICS: string;
	export const GDMSESSION: string;
	export const GITHUB_TOKEN: string;
	export const P9K_SSH: string;
	export const DISPLAY: string;
	export const VSCODE_IPC_HOOK: string;
	export const LESS: string;
	export const WAYLAND_DISPLAY: string;
	export const VSCODE_CWD: string;
	export const SDKMAN_CANDIDATES_API: string;
	export const OLDPWD: string;
	export const SDKMAN_PLATFORM: string;
	export const npm_package_json: string;
	export const DBUS_SESSION_BUS_ADDRESS: string;
	export const npm_config_user_agent: string;
	export const XDG_SESSION_TYPE: string;
	export const JOURNAL_STREAM: string;
	export const npm_node_execpath: string;
	export const SHLVL: string;
	export const USER: string;
	export const npm_config_noproxy: string;
	export const npm_config_npm_version: string;
	export const ANTIGRAVITY_AGENT: string;
	export const MANAGERPID: string;
	export const GIO_LAUNCHED_DESKTOP_FILE_PID: string;
	export const COLOR: string;
	export const ANTIGRAVITY_CSRF_TOKEN: string;
	export const XDG_CURRENT_DESKTOP: string;
	export const _: string;
	export const MANAGERPIDFDID: string;
	export const SBX_CHROME_API_RQ: string;
	export const npm_config_prefix: string;
	export const npm_config_userconfig: string;
	export const MEMORY_PRESSURE_WATCH: string;
	export const XDG_SESSION_CLASS: string;
	export const MEMORY_PRESSURE_WRITE: string;
	export const USERNAME: string;
	export const SDKMAN_BROKER_API: string;
	export const TERM: string;
	export const npm_config_cache: string;
	export const LSCOLORS: string;
	export const GNOME_DESKTOP_SESSION_ID: string;
	export const SHELL: string;
	export const npm_config_node_gyp: string;
	export const FC_FONTATIONS: string;
	export const GTK_MODULES: string;
	export const SDKMAN_CANDIDATES_DIR: string;
	export const VSCODE_PID: string;
	export const _P9K_SSH_TTY: string;
	export const INVOCATION_ID: string;
	export const NODE: string;
	export const XDG_MENU_PREFIX: string;
	export const npm_package_name: string;
}

/**
 * This module provides access to environment variables that are injected _statically_ into your bundle at build time and are _publicly_ accessible.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Static environment variables are [loaded by Vite](https://vitejs.dev/guide/env-and-mode.html#env-files) from `.env` files and `process.env` at build time and then statically injected into your bundle at build time, enabling optimisations like dead code elimination.
 * 
 * **_Public_ access:**
 * 
 * - This module _can_ be imported into client-side code
 * - **Only** variables that begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) (which defaults to `PUBLIC_`) are included
 * 
 * For example, given the following build time environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { ENVIRONMENT, PUBLIC_BASE_URL } from '$env/static/public';
 * 
 * console.log(ENVIRONMENT); // => throws error during build
 * console.log(PUBLIC_BASE_URL); // => "http://site.com"
 * ```
 * 
 * The above values will be the same _even if_ different values for `ENVIRONMENT` or `PUBLIC_BASE_URL` are set at runtime, as they are statically replaced in your code with their build time values.
 */
declare module '$env/static/public' {
	
}

/**
 * This module provides access to environment variables set _dynamically_ at runtime and that are limited to _private_ access.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Dynamic environment variables are defined by the platform you're running on. For example if you're using [`adapter-node`](https://github.com/sveltejs/kit/tree/main/packages/adapter-node) (or running [`vite preview`](https://svelte.dev/docs/kit/cli)), this is equivalent to `process.env`.
 * 
 * **_Private_ access:**
 * 
 * - This module cannot be imported into client-side code
 * - This module includes variables that _do not_ begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) _and do_ start with [`config.kit.env.privatePrefix`](https://svelte.dev/docs/kit/configuration#env) (if configured)
 * 
 * > [!NOTE] In `dev`, `$env/dynamic` includes environment variables from `.env`. In `prod`, this behavior will depend on your adapter.
 * 
 * > [!NOTE] To get correct types, environment variables referenced in your code should be declared (for example in an `.env` file), even if they don't have a value until the app is deployed:
 * >
 * > ```env
 * > MY_FEATURE_FLAG=
 * > ```
 * >
 * > You can override `.env` values from the command line like so:
 * >
 * > ```sh
 * > MY_FEATURE_FLAG="enabled" npm run dev
 * > ```
 * 
 * For example, given the following runtime environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { env } from '$env/dynamic/private';
 * 
 * console.log(env.ENVIRONMENT); // => "production"
 * console.log(env.PUBLIC_BASE_URL); // => undefined
 * ```
 */
declare module '$env/dynamic/private' {
	export const env: {
		SVELTEKIT_FORK: string;
		NODE_ENV: string;
		EDITOR: string;
		INIT_CWD: string;
		npm_config_global_prefix: string;
		QT_IM_MODULES: string;
		XDG_SESSION_EXTRA_DEVICE_ACCESS: string;
		XDG_DATA_DIRS: string;
		NVM_CD_FLAGS: string;
		npm_config_globalconfig: string;
		ANTIGRAVITY_TRAJECTORY_ID: string;
		VSCODE_CODE_CACHE_PATH: string;
		QT_IM_MODULE: string;
		GJS_DEBUG_OUTPUT: string;
		npm_config_init_module: string;
		QT_ACCESSIBILITY: string;
		npm_lifecycle_event: string;
		npm_package_version: string;
		GOPATH: string;
		ANTIGRAVITY_LS_ADDRESS: string;
		SSH_AUTH_SOCK: string;
		npm_lifecycle_script: string;
		SDKMAN_DIR: string;
		LS_COLORS: string;
		XDG_SESSION_DESKTOP: string;
		LANG: string;
		PYENV_ROOT: string;
		XDG_RUNTIME_DIR: string;
		GNOME_SETUP_DISPLAY: string;
		GIO_LAUNCHED_DESKTOP_FILE: string;
		XMODIFIERS: string;
		NVM_BIN: string;
		NVM_INC: string;
		npm_execpath: string;
		PYENV_SHELL: string;
		npm_command: string;
		GPG_AGENT_INFO: string;
		LOGNAME: string;
		ANTIGRAVITY_SOURCE_METADATA: string;
		ZSH: string;
		PATH: string;
		VSCODE_NLS_CONFIG: string;
		SYSTEMD_EXEC_PID: string;
		GDK_BACKEND: string;
		NVM_DIR: string;
		PAGER: string;
		DESKTOP_SESSION: string;
		BUN_INSTALL: string;
		XAUTHORITY: string;
		npm_config_local_prefix: string;
		CHROME_DESKTOP: string;
		ANTIGRAVITY_EDITOR_APP_ROOT: string;
		HOME: string;
		PWD: string;
		GJS_DEBUG_TOPICS: string;
		GDMSESSION: string;
		GITHUB_TOKEN: string;
		P9K_SSH: string;
		DISPLAY: string;
		VSCODE_IPC_HOOK: string;
		LESS: string;
		WAYLAND_DISPLAY: string;
		VSCODE_CWD: string;
		SDKMAN_CANDIDATES_API: string;
		OLDPWD: string;
		SDKMAN_PLATFORM: string;
		npm_package_json: string;
		DBUS_SESSION_BUS_ADDRESS: string;
		npm_config_user_agent: string;
		XDG_SESSION_TYPE: string;
		JOURNAL_STREAM: string;
		npm_node_execpath: string;
		SHLVL: string;
		USER: string;
		npm_config_noproxy: string;
		npm_config_npm_version: string;
		ANTIGRAVITY_AGENT: string;
		MANAGERPID: string;
		GIO_LAUNCHED_DESKTOP_FILE_PID: string;
		COLOR: string;
		ANTIGRAVITY_CSRF_TOKEN: string;
		XDG_CURRENT_DESKTOP: string;
		_: string;
		MANAGERPIDFDID: string;
		SBX_CHROME_API_RQ: string;
		npm_config_prefix: string;
		npm_config_userconfig: string;
		MEMORY_PRESSURE_WATCH: string;
		XDG_SESSION_CLASS: string;
		MEMORY_PRESSURE_WRITE: string;
		USERNAME: string;
		SDKMAN_BROKER_API: string;
		TERM: string;
		npm_config_cache: string;
		LSCOLORS: string;
		GNOME_DESKTOP_SESSION_ID: string;
		SHELL: string;
		npm_config_node_gyp: string;
		FC_FONTATIONS: string;
		GTK_MODULES: string;
		SDKMAN_CANDIDATES_DIR: string;
		VSCODE_PID: string;
		_P9K_SSH_TTY: string;
		INVOCATION_ID: string;
		NODE: string;
		XDG_MENU_PREFIX: string;
		npm_package_name: string;
		[key: `PUBLIC_${string}`]: undefined;
		[key: `${string}`]: string | undefined;
	}
}

/**
 * This module provides access to environment variables set _dynamically_ at runtime and that are _publicly_ accessible.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Dynamic environment variables are defined by the platform you're running on. For example if you're using [`adapter-node`](https://github.com/sveltejs/kit/tree/main/packages/adapter-node) (or running [`vite preview`](https://svelte.dev/docs/kit/cli)), this is equivalent to `process.env`.
 * 
 * **_Public_ access:**
 * 
 * - This module _can_ be imported into client-side code
 * - **Only** variables that begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) (which defaults to `PUBLIC_`) are included
 * 
 * > [!NOTE] In `dev`, `$env/dynamic` includes environment variables from `.env`. In `prod`, this behavior will depend on your adapter.
 * 
 * > [!NOTE] To get correct types, environment variables referenced in your code should be declared (for example in an `.env` file), even if they don't have a value until the app is deployed:
 * >
 * > ```env
 * > MY_FEATURE_FLAG=
 * > ```
 * >
 * > You can override `.env` values from the command line like so:
 * >
 * > ```sh
 * > MY_FEATURE_FLAG="enabled" npm run dev
 * > ```
 * 
 * For example, given the following runtime environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://example.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { env } from '$env/dynamic/public';
 * console.log(env.ENVIRONMENT); // => undefined, not public
 * console.log(env.PUBLIC_BASE_URL); // => "http://example.com"
 * ```
 * 
 * ```
 * 
 * ```
 */
declare module '$env/dynamic/public' {
	export const env: {
		[key: `PUBLIC_${string}`]: string | undefined;
	}
}
