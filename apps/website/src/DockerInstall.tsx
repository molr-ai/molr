import type { Locale } from './site-content';
import type { RuntimeMode } from './release-data';
import { InstallCommand } from './InstallCommand';

export function dockerInstallCommands(mode: RuntimeMode) {
  const sandbox = mode === 'sandbox';
  const name = sandbox ? 'molr-sandbox' : 'molr';
  const port = sandbox ? 8443 : 8787;
  const volume = sandbox ? 'molr-sandbox-data' : 'molr-data';
  const command = `docker run -d \\\n  --name ${name} \\\n  --init --restart unless-stopped \\\n  -v ${volume}:/data \\\n  -p ${port}:${port} \\\n${sandbox ? '  -e MOLR_MODE=sandbox \\\n' : ''}  ghcr.io/molr-ai/molr:latest`;
  const compose = `services:\n  ${name}:\n    image: ghcr.io/molr-ai/molr:latest\n    init: true\n    restart: unless-stopped\n${sandbox ? '    environment:\n      MOLR_MODE: sandbox\n' : ''}    ports:\n      - "${port}:${port}"\n    volumes:\n      - ${volume}:/data\n\nvolumes:\n  ${volume}:`;
  return { command, compose };
}

export function DockerInstall({ locale, mode }: { locale: Locale; mode: RuntimeMode }) {
  const zh = locale === 'zh', sandbox = mode === 'sandbox';
  const commands = dockerInstallCommands(mode);
  return <div>
    {(['docker', 'compose'] as const).map(method => {
      const compose = method === 'compose';
      return <section key={method} className="home-install-details home-install-docker-method" aria-label={compose ? 'Docker Compose' : 'Docker'}>
        <h3>{compose ? 'Docker Compose' : 'Docker'}</h3>
        <p>{compose
          ? (zh ? '将以下内容保存为 compose.yaml。' : 'Save this as compose.yaml.')
          : (zh ? '运行以下命令启动。' : 'Run this command to start.')}</p>
        <InstallCommand key={compose ? commands.compose : commands.command} command={compose ? commands.compose : commands.command} locale={locale}
          label={compose ? 'compose.yaml' : 'Terminal'} copyLabel={compose ? (zh ? '复制配置' : 'Copy configuration') : undefined} />
        {compose && <>
          <p>{zh ? '运行以下命令启动。' : 'Run this command to start.'}</p>
          <InstallCommand command="docker compose up -d" locale={locale} />
        </>}
        {sandbox ? <>
          <p>{zh ? '读取连接码。' : 'Read the connection code.'}</p>
          <InstallCommand command={`${compose ? 'docker compose' : 'docker'} exec molr-sandbox molr connection-code --config /data/sandbox.json`} locale={locale}
            copyLabel={zh ? '复制命令' : 'Copy command'} />
          <p>{zh ? '在后端打开「资源 → 沙盒 → 连接沙盒」，填写 https://<宿主机IP>:8443 并粘贴连接码，测试连接通过后保存。协议版本会自动校验。' : 'In the backend, open Resources → Sandboxes → Connect sandbox. Enter https://<host-IP>:8443 and paste the connection code, then test the connection and save. Protocol compatibility is checked automatically.'}</p>
        </> : <>
          <p>{zh ? '读取初始密码。' : 'Read the initial password.'}</p>
          <InstallCommand command={compose ? 'docker compose exec molr cat /data/initial-password' : 'docker exec molr cat /data/initial-password'} locale={locale}
            copyLabel={zh ? '复制命令' : 'Copy command'} />
          <p>{zh ? '启动后访问 ' : 'After startup, open '}<a href="http://127.0.0.1:8787">http://127.0.0.1:8787</a>{zh ? '，使用 owner 账号登录。' : ' and sign in as owner.'}</p>
        </>}
      </section>;
    })}
  </div>;
}
