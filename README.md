# ZenSSH

ZenSSH e uma TUI para Linux que gerencia aliases SSH com base no proprio OpenSSH.

## O que faz

- cadastro guiado de hosts
- persistencia local em `~/.config/zenssh/hosts.json`
- geracao de um arquivo gerenciado em `~/.config/zenssh/ssh_config`
- onboarding seguro na primeira execucao, antes de alterar o SSH do usuario
- descoberta de aliases em `~/.ssh/config` e seus arquivos `Include`
- resolucao da configuracao efetiva com `ssh -G` (inclui regras de `/etc/ssh/ssh_config`)
- descoberta opcional de candidatos em `/etc/hosts`
- descoberta de chaves privadas existentes em `~/.ssh` sem copiar seu conteudo
- suporte a multiplas identidades e certificados SSH por host
- hosts externos em modo somente leitura ou gerenciado pelo ZenSSH
- sincronizacao com deteccao de alteracoes, remocoes e conflitos
- validacao do arquivo gerado pelo proprio OpenSSH antes da instalacao
- gravacao transacional com rollback em caso de falha
- injecao idempotente de `Include ~/.config/zenssh/ssh_config` em `~/.ssh/config`, com backup
- conexao SSH por alias
- camada de diagnostico SSH com retry automatico para servidores legados (`ssh-rsa`, KEX antigos)
- geracao de chave `ed25519`
- envio de chave com `ssh-copy-id`

## Executar

```bash
go run ./cmd/zenssh
```

## Instalacao rapida

Depois que houver pelo menos uma release publicada, instale ou atualize para a versao mais recente com um unico comando:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | sh
```

Se `curl` nao estiver disponivel:

```bash
wget -qO- https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | sh
```

O instalador:

- suporta Linux `amd64` e `arm64`
- baixa a release mais recente diretamente do GitHub
- valida o arquivo usando `SHA256SUMS`
- instala OpenSSH quando a dependencia estiver ausente e o gerenciador de pacotes for reconhecido
- instala em `/usr/local/bin/zenssh`, usando `sudo` quando necessario
- usa `~/.local/bin` como fallback quando `sudo` nao estiver disponivel
- pode ser executado novamente para atualizar o ZenSSH

Para instalar uma versao especifica:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | ZENSSH_VERSION=v0.3.0 sh
```

Para escolher outro diretorio:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | ZENSSH_INSTALL_DIR="$HOME/.local/bin" sh
```

Confira a instalacao:

```bash
zenssh --version
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

Isso cria os assets estaveis usados pelo instalador:

```text
dist/zenssh_linux_amd64.tar.gz
dist/zenssh_linux_arm64.tar.gz
dist/SHA256SUMS
```

Cada pacote contem:

- binario `zenssh`
- `README.md`

Instalacao manual em qualquer distribuicao:

```bash
tar -xzf zenssh_linux_amd64.tar.gz
sudo install -m 0755 zenssh_0.1.0_linux_amd64/zenssh /usr/local/bin/zenssh
```

## Publicar uma release

O workflow de release e executado automaticamente para tags iniciadas por `v`:

```bash
git tag v0.3.0
git push origin v0.3.0
```

O GitHub Actions executa os testes, gera os binarios Linux `amd64` e `arm64`, cria os checksums e publica os tres arquivos na GitHub Release. O instalador rapido so funcionara depois da primeira tag/release publicada.

## Controles

- `a`: adicionar host
- `i`: importar aliases do `~/.ssh/config`
- `/`: buscar por alias, endereco, grupo ou origem
- `v`: mostrar diagnostico do host sem exibir material privado
- `m`: alternar host importado entre somente leitura e gerenciado
- `b`: restaurar `~/.ssh/config.zenssh.bak` com confirmacao
- `e`: editar host selecionado
- `d`: remover host selecionado
- `g`: gerar chave SSH
- `s`: enviar chave SSH
- `t`: validar explicitamente autenticacao por chave, sem permitir senha
- `Enter`: conectar via SSH
- `?`: abrir a ajuda completa de atalhos
- `q`: sair

Ao conectar, o ZenSSH encerra a TUI e substitui seu processo pelo OpenSSH. Quando a sessao remota terminar, o terminal volta diretamente ao shell; execute `zenssh` novamente para abrir o gerenciador.

## Primeira execucao

Na primeira abertura, o ZenSSH apenas le a configuracao existente e apresenta uma tela de revisao:

- aliases explicitos do OpenSSH ficam selecionados para importacao em modo somente leitura
- entradas de `/etc/hosts` aparecem como candidatos opcionais e ficam desmarcadas
- `Espaco` marca ou desmarca uma entrada
- `c` alterna entre as chaves descobertas para o host selecionado
- `o` alterna entre somente leitura e gerenciamento pelo ZenSSH
- `a` seleciona todas e `n` desmarca todas
- `Enter` confirma a importacao
- `Esc` cancela sem alterar `~/.ssh/config`

Ao confirmar, o arquivo original e preservado em `~/.ssh/config.zenssh.bak` antes da primeira alteracao. As chaves privadas continuam em seus caminhos originais; o ZenSSH guarda somente a referencia ao arquivo.

A tecla `i` executa a descoberta novamente para importar configuracoes adicionadas depois.
Ela tambem identifica alteracoes, remocoes e conflitos. Alteracoes seguras ficam selecionadas; conflitos exigem selecao explicita antes de substituir a versao local.

## Estados SSH no overview

A lista separa a confianca no servidor da autenticacao do usuario:

- `servidor:conhecido` significa que o destino foi encontrado em um arquivo `known_hosts`
- `chave:configurada` significa que existe uma identidade local associada
- `chave:envio-registrado` significa que o ZenSSH concluiu um `ssh-copy-id`
- `chave:validada` significa que um teste explicito autenticou usando chave, sem senha
- `chave:falhou` registra que o ultimo teste explicito nao validou a autenticacao

O teste da tecla `t` usa `BatchMode=yes`, desabilita senha e exige que o servidor ja seja conhecido. Ele pode gerar registro de conexao no servidor, mas nunca e executado automaticamente.

## Cadastro de host

- `Espaco`: alterna "enviar chave agora"
- `Ctrl+N`: adiciona outro arquivo de identidade SSH
- `Ctrl+D`: remove o arquivo de identidade selecionado
- "Salvar, testar e conectar" valida a configuracao e abre a sessao SSH
