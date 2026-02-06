import { Module, forwardRef } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { Task } from '../../entities/task.entity';
import { UserProfile } from '../../entities/user-profile.entity';
import { UserEmail } from '../../entities/user-email.entity';
import { ChatConversation } from '../../entities/chat-conversation.entity';
import { ChatMessage } from '../../entities/chat-message.entity';
import { TasksService } from './tasks.service';
import { TasksController } from './tasks.controller';
import { AuthModule } from '../auth/auth.module';
import { NotificationsModule } from '../notifications/notifications.module';
import { GraphModule } from '../graph/graph.module';
import { DatabaseModule } from '../../common/database/database.module';
import { ChatUiModule } from '../chat-ui/chat-ui.module';
import { UserModule } from '../user/user.module';
import { MergeSuggestionService } from './merge-suggestion.service';
import { MergeSuggestionPromptBuilder } from './merge-suggestion-prompt-builder.service';
import { MergeChatService } from './merge-chat.service';
import { SuggestionTaskHandlerService } from './suggestion-task-handler.service';

@Module({
  imports: [
    TypeOrmModule.forFeature([
      Task,
      UserProfile,
      UserEmail,
      ChatConversation,
      ChatMessage,
    ]),
    AuthModule,
    NotificationsModule, // For marking notifications as read when resolving tasks
    forwardRef(() => GraphModule), // For executing merge operations and suggestion tasks
    DatabaseModule, // For merge suggestion service
    ChatUiModule, // For LangGraph service
    UserModule, // For UserAccessService (cross-project task fetching)
  ],
  controllers: [TasksController],
  providers: [
    TasksService,
    MergeSuggestionService,
    MergeSuggestionPromptBuilder,
    MergeChatService,
    SuggestionTaskHandlerService,
  ],
  exports: [
    TasksService,
    MergeSuggestionService,
    MergeChatService,
    SuggestionTaskHandlerService,
  ],
})
export class TasksModule {}
