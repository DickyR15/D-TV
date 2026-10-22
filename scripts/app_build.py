from dataclasses import dataclass


@dataclass(frozen=True)
class BuildVariant:
    all_sources: bool = False

    @property
    def slug(self):
        return 'zhenguojian' if self.all_sources else 'hongguojian'

    @property
    def arguments(self):
        return ['--all-sources'] if self.all_sources else []

    @property
    def flutter_arguments(self):
        return ['--dart-define=ALL_SOURCES=' + str(self.all_sources).lower()]

    @property
    def linker_flags(self):
        return '-s -w -X duanjuapp/native/core.buildAllSources=' + str(self.all_sources).lower()


def add_variant_argument(parser):
    parser.add_argument('--all-sources', action='store_true',
                        help='構建包含全部站源的 D TV；預設構建僅紅果的 D TV')
