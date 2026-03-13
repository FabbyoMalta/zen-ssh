# ZenSSH

ZenSSH e uma TUI para Linux que gerencia aliases SSH com base no proprio OpenSSH.

## O que faz

- cadastro guiado de hosts
- persistencia local em `~/.config/zenssh/hosts.json`
- geracao de um arquivo gerenciado em `~/.config/zenssh/ssh_config`
- injecao automatica de `Include ~/.config/zenssh/ssh_config` em `~/.ssh/config`
- conexao SSH por alias
- camada de diagnostico SSH com retry automatico para servidores legados (`ssh-rsa`, KEX antigos)
- geracao de chave `ed25519`
- envio de chave com `ssh-copy-id`

## Executar

```bash
go run ./cmd/zenssh
```

## Build local

```bash
make build
./bin/zenssh
```

Para conferir a versao embutida no binario:

```bash
./bin/zenssh --version
```

## Empacotamento para distribuicoes Linux

O caminho mais simples para distribuir o ZenSSH em diferentes distribuicoes e publicar um binario estatico em `.tar.gz`.
Como o app chama utilitarios do sistema em tempo de execucao, o host ainda precisa ter OpenSSH instalado:

- Debian/Ubuntu: `openssh-client`
- Fedora/RHEL: `openssh-clients`
- Arch: `openssh`

Para gerar artefatos de release para Linux `amd64` e `arm64`:

```bash
make release VERSION=0.1.0
```

Isso cria arquivos em `dist/` no formato:

```text
dist/zenssh_0.1.0_linux_amd64.tar.gz
dist/zenssh_0.1.0_linux_arm64.tar.gz
```

Cada pacote contem:

- binario `zenssh`
- `README.md`

Instalacao manual em qualquer distribuicao:

```bash
tar -xzf zenssh_0.1.0_linux_amd64.tar.gz
sudo install -m 0755 zenssh_0.1.0_linux_amd64/zenssh /usr/local/bin/zenssh
```

## Estrategia de deploy recomendada

Para publicar agora, eu recomendo esta ordem:

1. subir os `.tar.gz` como assets de release
2. documentar a dependencia de OpenSSH por distribuicao
3. adicionar `.deb` e `.rpm` depois, se voce quiser instalacao nativa por gerenciador de pacotes

Para este projeto, isso costuma ser suficiente no primeiro deploy porque o binario Go e portavel e a unica dependencia real fica no sistema do usuario.

## Controles

- `a`: adicionar host
- `e`: editar host selecionado
- `d`: remover host selecionado
- `g`: gerar chave SSH
- `s`: enviar chave SSH
- `Enter`: conectar via SSH
- `q`: sair

## Cadastro de host

- `Espaco`: alterna "enviar chave agora" e "testar conexao ao salvar"
- `t`: testa o host preenchido sem sair do formulario
- o teste valida alcance, negociacao de algoritmos e persistencia de opcoes compativeis
