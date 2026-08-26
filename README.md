# ZenSSH

ZenSSH é um gerenciador de conexões SSH para terminal. Ele oferece uma TUI responsiva para organizar hosts, aliases, portas, usuários, grupos e identidades, reutilizando o OpenSSH instalado no sistema para todas as operações de rede e autenticação.

O projeto não implementa um cliente SSH próprio e não armazena chaves privadas. Sua função é organizar configurações existentes, gerar um arquivo compatível com OpenSSH e facilitar tarefas recorrentes como conexão, diagnóstico, geração e envio de chaves.

## Recursos

- descoberta inicial de configurações existentes;
- importação de aliases de `~/.ssh/config` e arquivos declarados com `Include`;
- resolução da configuração efetiva por meio de `ssh -G`, incluindo regras de `/etc/ssh/ssh_config`;
- descoberta opcional de hosts registrados em `/etc/hosts`;
- identificação de chaves privadas em `~/.ssh`, mantendo apenas a referência aos arquivos;
- cadastro e edição guiada de hosts;
- suporte a múltiplas identidades e certificados por host;
- agrupamento, seleção em massa e filtros por grupo;
- busca por alias, endereço, usuário, grupo ou origem;
- distinção entre hosts importados em modo somente leitura e hosts gerenciados;
- sincronização com detecção de alterações, remoções e conflitos;
- geração de chaves Ed25519 e envio com `ssh-copy-id`;
- validação explícita de autenticação por chave, sem fallback para senha;
- compatibilidade assistida com servidores que exigem algoritmos SSH legados;
- interface adaptável a terminais largos, médios e compactos;
- suporte a terminais claros, escuros e à variável `NO_COLOR`.

## Requisitos

- Linux em arquitetura `amd64` ou `arm64`;
- OpenSSH, incluindo `ssh`, `ssh-keygen` e `ssh-copy-id`;
- `curl` ou `wget` para instalação automática;
- `tar` e uma ferramenta de verificação SHA-256.

O instalador reconhece `apt`, `dnf`, `yum`, `pacman`, `apk` e `zypper` e pode instalar o cliente OpenSSH quando necessário.

## Instalação

### Instalação rápida

Com `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | sh
```

Com `wget`:

```bash
wget -qO- https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh | sh
```

O script baixa a release mais recente, verifica o arquivo com `SHA256SUMS` e instala o executável em `/usr/local/bin/zenssh`. Quando não há acesso administrativo, utiliza `~/.local/bin` como alternativa.

O mesmo comando pode ser executado novamente para atualizar uma instalação existente.

### Versão ou diretório específico

Para instalar uma versão específica:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh \
  | ZENSSH_VERSION=v0.3.0 sh
```

Para instalar somente para o usuário atual:

```bash
curl -fsSL https://raw.githubusercontent.com/FabbyoMalta/zen-ssh/main/install.sh \
  | ZENSSH_INSTALL_DIR="$HOME/.local/bin" sh
```

Verifique a instalação com:

```bash
zenssh --version
```

## Primeira execução

Inicie a interface executando:

```bash
zenssh
```

Na primeira execução, o ZenSSH analisa a configuração local e apresenta uma revisão antes de realizar qualquer alteração:

- aliases explícitos do OpenSSH são sugeridos em modo somente leitura;
- entradas de `/etc/hosts` aparecem como candidatos opcionais;
- identidades encontradas em `~/.ssh` podem ser associadas aos hosts;
- nenhuma chave privada é copiada ou enviada automaticamente;
- nenhuma configuração é gravada antes da confirmação do usuário.

Controles da revisão inicial:

| Tecla | Ação |
| --- | --- |
| `Espaço` | Marcar ou desmarcar uma entrada |
| `a` | Selecionar todas as entradas válidas |
| `n` | Limpar a seleção |
| `c` | Alternar a identidade associada |
| `o` | Alternar entre somente leitura e gerenciado |
| `Enter` | Confirmar a importação |
| `Esc` | Cancelar sem modificar a configuração |

Após a confirmação, o ZenSSH cria um backup em `~/.ssh/config.zenssh.bak` e adiciona de forma idempotente a diretiva:

```sshconfig
Include ~/.config/zenssh/ssh_config
```

## Uso da interface

### Navegação e hosts

| Tecla | Ação |
| --- | --- |
| `↑` / `k` | Selecionar o host anterior |
| `↓` / `j` | Selecionar o próximo host |
| `Enter` | Conectar ao host selecionado |
| `/` | Buscar hosts |
| `a` | Adicionar um host |
| `e` | Editar o host selecionado |
| `d` | Remover o host selecionado |
| `i` | Sincronizar novamente as configurações locais |
| `?` | Exibir a ajuda completa |
| `q` | Encerrar o ZenSSH |

### Grupos e seleção em massa

| Tecla | Ação |
| --- | --- |
| `[` / `]` | Navegar entre as abas de grupos |
| `Shift+S` | Entrar ou sair do modo de seleção |
| `Espaço` | Marcar ou desmarcar um host no modo de seleção |
| `Shift+G` | Atribuir um grupo aos hosts selecionados |
| `x` / `Esc` | Limpar a seleção e sair do modo de seleção |

O campo de grupo pode ser deixado vazio para remover os hosts selecionados de seus grupos atuais. A busca é aplicada dentro da aba ativa. As abas disponíveis incluem `Todos`, os grupos cadastrados e `Sem grupo`.

### SSH e diagnóstico

| Tecla | Ação |
| --- | --- |
| `v` | Exibir diagnóstico do host |
| `m` | Alternar host importado entre somente leitura e gerenciado |
| `g` | Gerar uma chave SSH Ed25519 |
| `s` | Enviar a chave pública com `ssh-copy-id` |
| `t` | Validar autenticação por chave sem permitir senha |
| `b` | Restaurar o backup de `~/.ssh/config` com confirmação |

Ao iniciar uma conexão, a TUI é encerrada e o processo do ZenSSH é substituído pelo OpenSSH. Quando a sessão remota termina, o terminal retorna diretamente ao shell. Esse comportamento evita que a interface tente recuperar ou redesenhar o terminal após a conexão.

## Cadastro de hosts

O formulário permite definir:

- alias;
- hostname ou endereço IP;
- porta;
- usuário;
- grupo;
- uma ou mais identidades SSH.
- o tipo de terminal enviado ao host remoto (`Padrão do sistema` ou `xterm`).

Controles adicionais do formulário:

| Tecla | Ação |
| --- | --- |
| `Tab` / `Shift+Tab` | Navegar entre os campos e ações |
| `Ctrl+N` | Adicionar outro arquivo de identidade |
| `Ctrl+D` | Remover o arquivo de identidade selecionado |
| `Espaço` | Alternar a opção selecionada (TERM remoto ou envio da chave) |
| `Enter` | Executar a ação selecionada |
| `Esc` | Cancelar o cadastro |

Se o Backspace ou outras teclas forem interpretados incorretamente em um host específico, edite esse host e altere **TERM remoto** para `xterm`. O ZenSSH inicia o OpenSSH com `TERM=xterm`, que é encaminhado automaticamente ao PTY remoto. Hosts existentes permanecem em `Padrão do sistema` até serem configurados.

## Modos de gerenciamento

Hosts descobertos em arquivos externos podem operar em dois modos:

- **somente leitura (`readonly`)**: o ZenSSH mantém a origem externa como autoridade e conecta utilizando o alias existente;
- **gerenciado (`managed`)**: o host passa a ser representado no arquivo SSH gerado pelo ZenSSH.

Hosts criados diretamente na interface utilizam o modo `manual`. A sincronização posterior compara fingerprints para identificar alterações seguras, remoções e conflitos.

## Estados exibidos

O dashboard separa dois conceitos que costumam ser confundidos:

- **confiança no servidor**: indica se o destino está registrado em `known_hosts`;
- **autenticação do usuário**: indica o estado da identidade usada para acessar o servidor.

Estados de autenticação possíveis:

| Estado | Significado |
| --- | --- |
| `sem-chave` | Nenhuma identidade específica foi associada |
| `configurada` | Existe uma identidade local associada |
| `envio-registrado` | O ZenSSH concluiu uma execução de `ssh-copy-id` |
| `validada` | Um teste explícito autenticou com chave e sem senha |
| `falhou` | O último teste explícito de autenticação falhou |

O teste executado por `t` utiliza `BatchMode=yes`, desabilita autenticação por senha e exige que o servidor já seja conhecido. Ele nunca é iniciado automaticamente.

## Arquivos e segurança

O ZenSSH mantém seus dados em:

| Caminho | Finalidade |
| --- | --- |
| `~/.config/zenssh/hosts.json` | Inventário e metadados dos hosts |
| `~/.config/zenssh/ssh_config` | Configuração OpenSSH gerada |
| `~/.config/zenssh/state.json` | Estado da primeira execução |
| `~/.ssh/config.zenssh.bak` | Backup criado antes da primeira alteração |

Medidas adotadas pelo projeto:

- chaves privadas nunca são copiadas para o diretório do ZenSSH;
- o inventário armazena somente caminhos de identidades;
- o arquivo gerado é validado pelo próprio OpenSSH antes da instalação;
- as gravações são transacionais e possuem rollback em caso de falha;
- a inclusão no arquivo principal é idempotente;
- testes de autenticação não permitem fallback para senha.

## Desenvolvimento

É necessário ter Go na versão declarada em `go.mod`.

Executar diretamente pelo código-fonte:

```bash
go run ./cmd/zenssh
```

Compilar:

```bash
make build
./bin/zenssh
```

Executar os testes:

```bash
make test
```

## Releases

Para gerar localmente os pacotes Linux para `amd64` e `arm64`:

```bash
make release VERSION=v0.3.0
```

Os artefatos são gravados em `dist/`:

```text
dist/zenssh_linux_amd64.tar.gz
dist/zenssh_linux_arm64.tar.gz
dist/SHA256SUMS
```

Tags com prefixo `v` acionam o workflow de release do GitHub Actions:

```bash
git tag -a v0.3.0 -m "ZenSSH v0.3.0"
git push origin v0.3.0
```

O workflow executa os testes, compila os binários, gera os checksums e publica uma GitHub Release com notas automáticas.
